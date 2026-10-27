import assert from 'node:assert/strict';
import { test } from 'node:test';
import { execFile } from 'node:child_process';
import { request as httpRequest } from 'node:http';
import { promisify } from 'node:util';
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash, createPublicKey, ECDH, verify, webcrypto } from 'node:crypto';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { InMemoryTransport } from '@modelcontextprotocol/sdk/inMemory.js';
import { WebStandardStreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js';
import { CallToolRequestSchema, ListToolsRequestSchema, type Tool } from '@modelcontextprotocol/sdk/types.js';
import { configure, safeError } from '../src/config.js';
import { callbackListener } from '../src/callback.js';
import { ReahOAuth } from '../src/oauth.js';
import { RemoteReah, reviewedTools, serve } from '../src/proxy.js';
import { generateDeviceKey, signApprovalPayload } from '../src/signing.js';
import type { Session, Store } from '../src/store.js';

class MemoryStore implements Store {
  state: Session | null = null;
  private pending: Promise<unknown> = Promise.resolve();
  async read() { return structuredClone(this.state); }
  async write(value: Session) { this.state = structuredClone(value); }
  async remove() { this.state = null; }
  exclusive<T>(action: () => Promise<T>): Promise<T> {
    const next = this.pending.then(action);
    this.pending = next.catch(() => undefined);
    return next;
  }
}

const config = configure();
const tools: Tool[] = [
  { name: 'reah_list_entities', description: 'Names and IDs of the authorized businesses.',
    inputSchema: { type: 'object', properties: {}, additionalProperties: false },
    annotations: { readOnlyHint: true } },
  { name: 'reah_list_balances', description: 'Amount strings in instrument units; decimals describe precision.',
    inputSchema: { type: 'object', properties: { entityId: { type: 'string' } }, required: ['entityId'], additionalProperties: false },
    annotations: { readOnlyHint: true } },
  { name: 'reah_send_money', inputSchema: { type: 'object' }, annotations: { readOnlyHint: false } },
];

async function fixture() {
  const store = new MemoryStore();
  let registrationCount = 0;
  let exchanges = 0;
  let refreshes = 0;
  let revokes = 0;
  let rotationLost = false;
  let revokeUnavailable = false;
  let challenge = '';
  let redirectUri = '';
  let activeToken = '';
  let activeRefresh = '';
  let selection = 'company-a';
  let remoteCalls = 0;
  let metadataOverride: Record<string, unknown> = {};
  const seenAccess: string[] = [];
  const browserResponses: Promise<void>[] = [];
  const transports: WebStandardStreamableHTTPServerTransport[] = [];
  const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
    status, headers: { 'content-type': 'application/json' },
  });
  function token() {
    activeToken = `access-${exchanges}-${refreshes}`;
    activeRefresh = `refresh-${exchanges}-${refreshes}`;
    return json({ access_token: activeToken, refresh_token: activeRefresh,
      expires_in: 900, token_type: 'Bearer', scope: 'reah.read' });
  }
  const mockFetch: typeof fetch = async (input, init) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.href : input.url);
    assert.equal(url.origin, config.origin);
    assert.equal(init?.redirect, 'error');
    switch (url.pathname) {
      case '/.well-known/oauth-protected-resource/mcp':
        return json({ resource: config.server, authorization_servers: [config.origin] });
      case '/.well-known/oauth-authorization-server':
        return json({ issuer: config.origin, authorization_endpoint: config.origin + '/mcp/oauth/authorize',
          token_endpoint: config.origin + '/mcp/oauth/token', revocation_endpoint: config.origin + '/mcp/oauth/revoke',
          registration_endpoint: config.origin + '/mcp/oauth/register', code_challenge_methods_supported: ['S256'],
          scopes_supported: ['reah.read'], ...metadataOverride });
      case '/mcp/oauth/register': {
        registrationCount++;
        const body = JSON.parse(init!.body as string);
        assert.equal(body.token_endpoint_auth_method, 'none');
        assert.equal(body.scope, 'reah.read');
        assert.deepEqual(body.redirect_uris, [`http://127.0.0.1/callback/reah-mcp/${config.id.slice(0, 24)}`]);
        return json({ client_id: 'local-client-1' }, 201);
      }
      case '/mcp/oauth/token': {
        const body = new URLSearchParams(init!.body as string);
        assert.equal(body.get('client_id'), 'local-client-1');
        assert.equal(body.get('resource'), config.server);
        if (body.get('grant_type') === 'authorization_code') {
          assert.equal(body.get('code'), 'one-time-test-code');
          assert.equal(createHash('sha256').update(body.get('code_verifier')!).digest('base64url'), challenge);
          assert.equal(body.get('redirect_uri'), redirectUri);
          exchanges++;
        } else {
          assert.equal(store.state?.tokens, undefined, 'old refresh token must be durably invalidated before sending');
          assert.equal(body.get('refresh_token'), activeRefresh);
          refreshes++;
          if (rotationLost) { token(); throw new Error('Lost response with secret-token'); }
        }
        return token();
      }
      case '/mcp/oauth/revoke': {
        if (revokeUnavailable) throw new Error('Network unavailable');
        const body = new URLSearchParams(init!.body as string);
        assert.equal(body.get('client_id'), 'local-client-1');
        assert.equal(body.get('token'), activeRefresh);
        revokes++; activeToken = ''; activeRefresh = '';
        return json({});
      }
      case '/mcp': {
        assert.equal(new Headers(init?.headers).get('Authorization'), `Bearer ${activeToken}`);
        seenAccess.push(activeToken);
        if (init?.method !== 'POST') return new Response(null, { status: 405 });
        const transport = new WebStandardStreamableHTTPServerTransport({ enableJsonResponse: true });
        transports.push(transport);
        const upstream = new Server({ name: 'mock-reah-gateway', version: '1' }, { capabilities: { tools: {} } });
        upstream.setRequestHandler(ListToolsRequestSchema, async () => ({ tools }));
        upstream.setRequestHandler(CallToolRequestSchema, async req => {
          remoteCalls++;
          if (req.params.name === 'reah_list_entities') return { content: [{ type: 'text', text: selection }],
            structuredContent: { entities: [{ id: selection, name: 'Selected business' }] } };
          assert.equal(req.params.arguments?.entityId, selection);
          return { content: [{ type: 'text', text: 'balance' }],
            structuredContent: { amount: '1234500', decimals: 6, currency: 'USDC' } };
        });
        await upstream.connect(transport);
        return transport.handleRequest(new Request(url, init));
      }
      default: throw new Error('Unexpected endpoint');
    }
  };
  const oauth = new ReahOAuth(config, store, mockFetch);
  const browser = async (href: string) => {
    const url = new URL(href);
    assert.equal(url.origin, config.origin);
    assert.equal(url.searchParams.get('scope'), 'reah.read');
    assert.equal(url.searchParams.get('code_challenge_method'), 'S256');
    challenge = url.searchParams.get('code_challenge')!;
    redirectUri = url.searchParams.get('redirect_uri')!;
    const callback = new URL(redirectUri);
    callback.search = new URLSearchParams({ state: url.searchParams.get('state')!, code: 'one-time-test-code', iss: config.origin }).toString();
    // Simulated browser follows an actual loopback HTTP callback. Success waits for token persistence.
    browserResponses.push(fetch(callback).then(async response => {
      assert.equal(response.status, 200);
      assert.match(await response.text(), /authentication complete/);
      assert.ok(store.state?.tokens);
    }));
  };
  return { store, oauth, browser, mockFetch, seenAccess,
    login: async () => { await oauth.login(browser); await Promise.all(browserResponses); },
    counts: () => ({ registrationCount, exchanges, refreshes, revokes, remoteCalls }),
    loseRotation: () => { rotationLost = true; },
    failRevocation: (fail: boolean) => { revokeUnavailable = fail; },
    select: (entity: string) => { selection = entity; },
    metadata: (value: Record<string, unknown>) => { metadataOverride = value; },
    close: async () => { await Promise.all(transports.map(t => t.close())); },
  };
}

test('complete local flow: browser OAuth, MCP initialize/list/call, refresh, new consent, logout', async () => {
  const f = await fixture();
  let local: Awaited<ReturnType<typeof serve>> | undefined;
  const host = new Client({ name: 'test-agent', version: '1' });
  try {
    await f.login();
    assert.equal((await f.oauth.status()).signingKey?.enrollment, 'not_supported');
    const pair = InMemoryTransport.createLinkedPair();
    local = await serve(new RemoteReah(f.oauth, f.mockFetch), pair[1]);
    await host.connect(pair[0]);
    const catalog = await host.listTools();
    assert.deepEqual(catalog.tools, tools.slice(0, 2), 'schemas, units and descriptions must pass through unchanged');
    const entities = await host.callTool({ name: 'reah_list_entities', arguments: {} });
    assert.deepEqual(entities.structuredContent, { entities: [{ id: 'company-a', name: 'Selected business' }] });
    const balances = await host.callTool({ name: 'reah_list_balances', arguments: { entityId: 'company-a' } });
    assert.deepEqual(balances.structuredContent, { amount: '1234500', decimals: 6, currency: 'USDC' });
    const before = f.counts().remoteCalls;
    assert.equal((await host.callTool({ name: 'reah_send_money', arguments: {} })).isError, true);
    assert.equal(f.counts().remoteCalls, before);
    f.store.state!.tokens!.expiresAt = 0;
    await host.callTool({ name: 'reah_list_entities', arguments: {} });
    assert.equal(f.counts().refreshes, 1);
    f.select('company-b');
    await f.login();
    const updated = await host.callTool({ name: 'reah_list_entities', arguments: {} });
    assert.deepEqual(updated.structuredContent, { entities: [{ id: 'company-b', name: 'Selected business' }] });
    assert.equal(f.counts().registrationCount, 1);
    assert.equal(f.counts().revokes, 1);
    await f.oauth.logout();
    assert.equal(f.counts().revokes, 2);
    assert.equal(f.store.state, null);
    const disconnected = await host.callTool({ name: 'reah_list_entities', arguments: {} });
    assert.equal(disconnected.isError, true);
    assert.match(JSON.stringify(disconnected), /not connected/);
  } finally { await host.close(); await local?.close(); await f.close(); }
});

test('simultaneous clients refresh once and reload the rotated credentials', async () => {
  const f = await fixture();
  await f.login();
  f.store.state!.tokens!.expiresAt = 0;
  const second = new ReahOAuth(config, f.store, f.mockFetch);
  const tokens = await Promise.all([f.oauth.accessToken(), second.accessToken(), f.oauth.accessToken()]);
  assert.equal(new Set(tokens).size, 1);
  assert.equal(f.counts().refreshes, 1);
});

test('lost refresh response is not replayed and sensitive errors are not returned', async () => {
  const f = await fixture();
  await f.login();
  f.store.state!.tokens!.expiresAt = 0;
  f.loseRotation();
  await assert.rejects(f.oauth.accessToken(), /did not complete/);
  await assert.rejects(f.oauth.accessToken(), /not connected/);
  assert.equal(f.counts().refreshes, 1);
  assert.doesNotMatch(safeError(new Error('secret-token')), /secret-token/);
});

test('discovery cannot redirect token requests to another origin', async () => {
  const f = await fixture();
  f.metadata({ token_endpoint: 'https://attacker.example/token' });
  await assert.rejects(f.oauth.login(f.browser), /unexpected endpoint/);
  assert.equal(f.counts().registrationCount, 0);
});

test('failed revocation disables reads and remains retryable instead of reporting false success', async () => {
  const f = await fixture();
  await f.login();
  f.failRevocation(true);
  await assert.rejects(f.oauth.logout(), /could not be confirmed/);
  await assert.rejects(f.oauth.accessToken(), /not connected/);
  assert.equal((await f.oauth.status()).revocationPending, true);
  f.failRevocation(false);
  await f.oauth.logout();
  assert.equal(f.counts().revokes, 1);
  assert.equal(f.store.state, null);
});

test('credential persistence failure stops login before launching a browser', async () => {
  const f = await fixture();
  f.store.write = async () => { throw new Error('locked'); };
  let launched = false;
  await assert.rejects(f.oauth.login(async () => { launched = true; }));
  assert.equal(launched, false);
});

test('loopback callback rejects wrong state, duplicate parameters, issuer mismatch and POST', async () => {
  const listener = await callbackListener('correct-state', config.origin, '/callback/test', 2000);
  try {
    for (const query of [
      'state=bad&code=code', 'state=correct-state&state=correct-state&code=code',
      'state=correct-state&code=code&iss=https://attacker.example',
    ]) assert.equal((await fetch(`${listener.redirectUri}?${query}`)).status, 400);
    assert.equal((await fetch(listener.redirectUri, { method: 'POST' })).status, 400);
    const response = fetch(`${listener.redirectUri}?state=correct-state&code=valid`);
    const callback = await listener.result;
    assert.equal(callback.code, 'valid'); callback.finish(true);
    assert.equal((await response).status, 200);
    assert.equal((await fetch(`${listener.redirectUri}?state=correct-state&code=replay`)).status, 400);
  } finally { listener.close(); }
});

test('denial and callback expiry fail cleanly', async () => {
  const denied = await callbackListener('state', config.origin, '/callback', 2000);
  try {
    const response = fetch(`${denied.redirectUri}?state=state&error=access_denied`);
    await assert.rejects(denied.result, /denied/);
    assert.equal((await response).status, 400);
  } finally { denied.close(); }
  const expired = await callbackListener('state', config.origin, '/callback', 20);
  await assert.rejects(expired.result, /timed out/);
  expired.close();
});

test('an unresponsive OS browser launcher cannot bypass the login deadline', async () => {
  const f = await fixture();
  await assert.rejects(f.oauth.login(() => new Promise<void>(() => undefined), 20), /timed out/);
  assert.equal(f.store.state?.tokens, undefined);
});

test('signatures match Web P-256, compressed key and IEEE P1363 base64-text protocol', async () => {
  const key = generateDeviceKey();
  const payload = Buffer.from([10, 3, 97, 98, 99]).toString('base64');
  const signed = signApprovalPayload(key, payload);
  assert.equal(signed.publicKey.length, 66);
  assert.equal(signed.signature.length, 128);
  const publicKey = createPublicKey(key.privateKey);
  assert.equal(verify('sha256', Buffer.from(payload), { key: publicKey, dsaEncoding: 'ieee-p1363' }, Buffer.from(signed.signature, 'hex')), true);
  assert.equal(verify('sha256', Buffer.from(payload, 'base64'), { key: publicKey, dsaEncoding: 'ieee-p1363' }, Buffer.from(signed.signature, 'hex')), false);
  const rawKey = ECDH.convertKey(Buffer.from(key.publicKey, 'hex'), 'prime256v1', undefined, undefined, 'uncompressed');
  const webKey = await webcrypto.subtle.importKey('raw', new Uint8Array(rawKey as Buffer), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify']);
  assert.equal(await webcrypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, webKey,
    new Uint8Array(Buffer.from(signed.signature, 'hex')), new TextEncoder().encode(payload)), true);
  assert.throws(() => signApprovalPayload(key, 'arbitrary text'));
  assert.throws(() => signApprovalPayload({ ...key, publicKey: generateDeviceKey().publicKey }, payload));
});

test('tool catalog fails closed for unknown names, missing read annotations and duplicate names', () => {
  assert.deepEqual(reviewedTools([{ ...tools[0]!, annotations: {} },
    { name: 'unknown', inputSchema: { type: 'object' }, annotations: { readOnlyHint: true } }]), []);
  assert.throws(() => reviewedTools([tools[0]!, tools[0]!]));
});

test('server and profile cannot inject credentials, callback paths or HTTP destinations', () => {
  for (const url of ['http://platform.stg.reah.com/mcp', 'https://user:secret@example.com/mcp',
    'https://example.com/mcp?x=1', 'https://example.com/mcp#fragment', 'https://example.com/other']) {
    assert.throws(() => configure(url));
  }
  assert.throws(() => configure(config.server, '../profile'));
  assert.notEqual(configure(config.server, 'other').id, config.id);
});

test('real stdio subprocess completes MCP initialization without touching credentials', async () => {
  const host = new Client({ name: 'stdio-smoke', version: '1' });
  const cli = new URL('../src/cli.js', import.meta.url).pathname;
  const transport = new StdioClientTransport({ command: process.execPath, args: [cli, 'serve'], stderr: 'pipe' });
  let stderr = '';
  transport.stderr?.on('data', chunk => { stderr += chunk.toString(); });
  try {
    await host.connect(transport);
    assert.equal(host.getServerVersion()?.name, 'reah-local-mcp');
    await host.ping();
    assert.equal(stderr, '');
  } finally { await host.close(); }
});

test('actual credential lock serializes separate Node.js processes', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'reah-lock-test-'));
  const counter = join(directory, 'counter');
  const script = `
    import { readFile, writeFile } from 'node:fs/promises';
    const { KeychainStore } = await import(process.argv[1]);
    const { configure } = await import(process.argv[2]);
    const store = new KeychainStore(configure(), process.argv[3]);
    await store.exclusive(async () => {
      const path = process.argv[3] + '/counter';
      const previous = Number(await readFile(path, 'utf8'));
      await new Promise(resolve => setTimeout(resolve, 75));
      await writeFile(path, String(previous + 1));
    });
  `;
  try {
    await writeFile(counter, '0');
    const run = () => promisify(execFile)(process.execPath, ['--input-type=module', '-e', script,
      new URL('../src/store.js', import.meta.url).href, new URL('../src/config.js', import.meta.url).href, directory]);
    await Promise.all([run(), run(), run()]);
    assert.equal(await readFile(counter, 'utf8'), '3');
  } finally { await rm(directory, { recursive: true, force: true }); }
});


test('malformed callback request cannot terminate the login listener', async () => {
  const listener = await callbackListener('state', config.origin, '/callback', 2000);
  try {
    const target = new URL(listener.redirectUri);
    const code = await new Promise<number | undefined>((resolve, reject) => {
      const req = httpRequest({ hostname: target.hostname, port: target.port, path: '///', method: 'GET' }, res => {
        res.resume(); res.on('end', () => resolve(res.statusCode));
      });
      req.on('error', reject); req.end();
    });
    assert.equal(code, 400);
    const response = fetch(`${listener.redirectUri}?state=state&code=valid`);
    const callback = await listener.result; callback.finish(true);
    assert.equal((await response).status, 200);
  } finally { listener.close(); }
});


test('losing the credential lock terminates before a paused action can write', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'reah-lock-loss-'));
  const marker = join(directory, 'unsafe-write');
  const script = `
    import { writeFile, rm } from 'node:fs/promises';
    const { KeychainStore } = await import(process.argv[1]);
    const { configure } = await import(process.argv[2]);
    const config = configure();
    const directory = process.argv[3];
    const store = new KeychainStore(config, directory);
    await store.exclusive(async () => {
      await rm(directory + '/' + config.id + '.lock', { recursive: true });
      await new Promise(resolve => setTimeout(resolve, 7000));
      await writeFile(directory + '/unsafe-write', 'overwritten');
    });
  `;
  try {
    await assert.rejects(promisify(execFile)(process.execPath, ['--input-type=module', '-e', script,
      new URL('../src/store.js', import.meta.url).href, new URL('../src/config.js', import.meta.url).href, directory]),
      (error: unknown) => error instanceof Error && /credential lock was lost/.test(String((error as Error & { stderr?: string }).stderr)));
    await assert.rejects(readFile(marker), { code: 'ENOENT' });
  } finally { await rm(directory, { recursive: true, force: true }); }
});
