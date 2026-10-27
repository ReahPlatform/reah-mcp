import { createHash, randomBytes } from 'node:crypto';
import type { Config } from './config.js';
import { LocalError, SCOPE } from './config.js';
import type { Store, Session, Tokens } from './store.js';
import { generateDeviceKey } from './signing.js';
import { parseEnrollment, signEnrollment, enrollmentReceipt } from './enrollment.js';
import { callbackListener } from './callback.js';

type Document = Record<string, unknown>;
type Fetch = typeof globalThis.fetch;

export class ReahOAuth {
  constructor(readonly config: Config, private readonly store: Store,
    private readonly fetch: Fetch = globalThis.fetch) {}

  private async request(path: string, body?: URLSearchParams | Document, accessToken?: string): Promise<Document> {
    try {
      const response = await this.fetch(this.config.origin + path, {
        method: body ? 'POST' : 'GET', redirect: 'error', signal: AbortSignal.timeout(20_000),
        headers: { Accept: 'application/json', ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}), ...(body ? { 'Content-Type': body instanceof URLSearchParams
          ? 'application/x-www-form-urlencoded' : 'application/json' } : {}) },
        ...(body ? { body: body instanceof URLSearchParams ? body.toString() : JSON.stringify(body) } : {}),
      });
      if (!response.ok) {
        await response.body?.cancel();
        throw new LocalError(`Reah OAuth request failed (HTTP ${response.status}). Reconnect if necessary.`);
      }
      if (path.endsWith('/revoke')) { await response.body?.cancel(); return {}; }
      const reader = response.body?.getReader();
      if (!reader) throw new Error('Empty response');
      const chunks: Uint8Array[] = [];
      let size = 0;
      try {
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          size += value.byteLength;
          if (size > 65_536) throw new Error('Oversized response');
          chunks.push(value);
        }
      } finally { await reader.cancel(); }
      const raw = Buffer.concat(chunks).toString('utf8');
      const value: unknown = JSON.parse(raw);
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid response');
      return value as Document;
    } catch (error) {
      if (error instanceof LocalError) throw error;
      throw new LocalError('Reah OAuth request did not complete. Start a new login if token exchange or refresh was interrupted.');
    }
  }

  async discover(enrollKey = false): Promise<void> {
    const resource = await this.request('/.well-known/oauth-protected-resource/mcp');
    const metadata = await this.request('/.well-known/oauth-authorization-server');
    if (resource.resource !== this.config.server || !Array.isArray(resource.authorization_servers) ||
        !resource.authorization_servers.includes(this.config.origin) || metadata.issuer !== this.config.origin ||
        !Array.isArray(metadata.code_challenge_methods_supported) || !metadata.code_challenge_methods_supported.includes('S256') ||
        !Array.isArray(metadata.scopes_supported) || !metadata.scopes_supported.includes(SCOPE)) {
      throw new LocalError('Reah OAuth discovery does not match the configured MCP server.');
    }
    if (enrollKey && metadata.reah_credential_enrollment_supported !== true) {
      throw new LocalError('This Reah server does not support credential preparation. Use ordinary login for read-only access.');
    }
    for (const [field, name] of Object.entries({ authorization_endpoint: 'authorize', token_endpoint: 'token',
      revocation_endpoint: 'revoke', registration_endpoint: 'register' })) {
      if (metadata[field] !== `${this.config.origin}/mcp/oauth/${name}`) {
        throw new LocalError('Reah OAuth discovery contains an unexpected endpoint.');
      }
    }
  }

  async login(openBrowser: (url: string) => Promise<void>, timeoutMs = 300_000, enrollKey = false): Promise<void> {
    await this.discover(enrollKey);
    await this.store.exclusive(async () => {
      let session = await this.store.read();
      const path = `/callback/reah-mcp/${this.config.id.slice(0, 24)}`;
      if (!session) {
        const registration = await this.request('/mcp/oauth/register', {
          client_name: 'Reah Local MCP', redirect_uris: [`http://127.0.0.1${path}`],
          grant_types: ['authorization_code', 'refresh_token'], response_types: ['code'],
          token_endpoint_auth_method: 'none', scope: SCOPE,
        });
        if (typeof registration.client_id !== 'string' || !registration.client_id || registration.client_id.length > 4096) {
          throw new LocalError('Reah returned invalid client registration.');
        }
        session = { version: 1, server: this.config.server, clientId: registration.client_id, deviceKey: generateDeviceKey() };
        await this.store.write(session);
      }
      // Invalidate local credentials first so running chats cannot keep using an old selection.
      const { tokens: previousTokens, pendingRevocation, enrollment: _enrollment,
        credentialEnrollmentRequested: _requested, ...oldConnection } = session;
      const unconnected: Session = { ...oldConnection, deviceKey: generateDeviceKey(),
        credentialEnrollmentRequested: enrollKey };
      const previous = previousTokens?.refreshToken ?? pendingRevocation;
      await this.store.write({ ...unconnected, ...(previous ? { pendingRevocation: previous } : {}) });
      if (previous) await this.revoke(session.clientId, previous);
      await this.store.write(unconnected);
      const verifier = randomBytes(32).toString('base64url');
      const state = randomBytes(32).toString('base64url');
      const listener = await callbackListener(state, this.config.origin, path, timeoutMs);
      try {
        const url = new URL(this.config.origin + '/mcp/oauth/authorize');
        url.search = new URLSearchParams({ response_type: 'code', client_id: session.clientId,
          redirect_uri: listener.redirectUri, scope: SCOPE, resource: this.config.server, state,
          code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256' }).toString();
        if (enrollKey) url.searchParams.set('credential_public_key', unconnected.deviceKey.publicKey);
        const callback = await Promise.race([
          openBrowser(url.href).then(() => listener.result), listener.result,
        ]);
        let authenticated: Session;
        try {
          const tokens = this.parseTokens(await this.request('/mcp/oauth/token', new URLSearchParams({
            grant_type: 'authorization_code', client_id: session.clientId, resource: this.config.server,
            code: callback.code, code_verifier: verifier, redirect_uri: listener.redirectUri,
          })));
          authenticated = { ...unconnected, tokens };
          await this.store.write(authenticated);
          callback.finish(true);
        } catch (error) { callback.finish(false); throw error; }
        if (enrollKey) await this.prepareKey(authenticated);
      } finally { listener.close(); }
    });
  }


  private async prepareKey(session: Session): Promise<void> {
    if (!session.credentialEnrollmentRequested || !session.tokens) {
      throw new LocalError('Start a new login with --enroll-key to bind a fresh key before browser authorization.');
    }
    const prepared = parseEnrollment(await this.request('/mcp/credentials/prepare', {}, session.tokens.accessToken), this.config, session);
    const pending = { ...session, enrollment: enrollmentReceipt(prepared) };
    await this.store.write(pending);
    if (prepared.state === 'awaiting_provider') return;
    if (prepared.state !== 'awaiting_key_proof') throw new LocalError('Credential preparation has expired or was revoked. Start a new login with --enroll-key.');
    const signature = signEnrollment(prepared, pending);
    const proven = parseEnrollment(await this.request('/mcp/credentials/prove', { signature }, session.tokens.accessToken), this.config, pending);
    if (proven.state !== 'awaiting_provider') throw new LocalError('Credential possession was not confirmed. Retry enroll-key or reconnect.');
    await this.store.write({ ...pending, enrollment: enrollmentReceipt(proven) });
  }

  async enrollKey(): Promise<void> {
    const token = await this.accessToken();
    await this.store.exclusive(async () => {
      const session = await this.store.read();
      if (session?.tokens?.accessToken !== token) throw new LocalError('The connection changed. Retry enroll-key.');
      await this.prepareKey(session);
    });
  }

  private parseTokens(value: Document): Tokens {
    if (typeof value.access_token !== 'string' || !value.access_token || /[^\x21-\x7e]/.test(value.access_token) ||
        typeof value.refresh_token !== 'string' || !value.refresh_token ||
        value.token_type !== 'Bearer' || value.scope !== SCOPE || typeof value.expires_in !== 'number' ||
        !Number.isSafeInteger(value.expires_in) || value.expires_in < 1 || value.expires_in > 86_400) {
      throw new LocalError('Reah returned an invalid token response. Reconnect to continue.');
    }
    return { accessToken: value.access_token, refreshToken: value.refresh_token,
      expiresAt: Date.now() + value.expires_in * 1000 };
  }

  async accessToken(): Promise<string> {
    return this.store.exclusive(async () => {
      const session = await this.store.read();
      if (!session?.tokens) throw new LocalError('Reah is not connected. Run reah-mcp login with the same server and profile.');
      if (session.tokens.expiresAt > Date.now() + 30_000) return session.tokens.accessToken;
      const { tokens, pendingRevocation: _, ...unconnected } = session;
      // Persist consumption before sending. A lost response or crash must never replay a rotated token.
      await this.store.write({ ...unconnected, pendingRevocation: tokens.refreshToken });
      const fresh = this.parseTokens(await this.request('/mcp/oauth/token', new URLSearchParams({
        grant_type: 'refresh_token', client_id: session.clientId, resource: this.config.server,
        refresh_token: tokens.refreshToken,
      })));
      await this.store.write({ ...unconnected, tokens: fresh });
      return fresh.accessToken;
    });
  }

  private async revoke(clientId: string, token: string) {
    await this.request('/mcp/oauth/revoke', new URLSearchParams({ client_id: clientId,
      token, token_type_hint: 'refresh_token', resource: this.config.server }));
  }

  async logout(): Promise<void> {
    await this.store.exclusive(async () => {
      const session = await this.store.read();
      if (!session) return;
      // Disable local access even if remote revocation is interrupted.
      const { tokens, pendingRevocation, ...unconnected } = session;
      const token = tokens?.refreshToken ?? pendingRevocation;
      await this.store.write({ ...unconnected, ...(token ? { pendingRevocation: token } : {}) });
      if (token) {
        try { await this.revoke(session.clientId, token); }
        catch {
          throw new LocalError('Local access is disabled, but remote revocation could not be confirmed. Disconnect this app in Reah Settings.');
        }
      }
      await this.store.remove();
    });
  }

  async status() {
    return this.store.exclusive(async () => {
      const session = await this.store.read();
      return { server: this.config.server, profile: this.config.profile,
        credentialsSaved: !!session?.tokens, revocationPending: !!session?.pendingRevocation,
        accessExpiresAt: session?.tokens ? new Date(session.tokens.expiresAt).toISOString() : null,
        signingKey: session ? { publicKey: session.deviceKey.publicKey, enrollment: session.enrollment?.state ?? (session.credentialEnrollmentRequested ? 'not_prepared' : 'not_supported') } : null };
    });
  }
}
