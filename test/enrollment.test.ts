import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createHash, createPublicKey, verify } from 'node:crypto';
import { configure } from '../src/config.js';
import { parseEnrollment, signEnrollment, enrollmentReceipt } from '../src/enrollment.js';
import { generateDeviceKey } from '../src/signing.js';
import { ReahOAuth } from '../src/oauth.js';
import type { Session, Store } from '../src/store.js';

const config = configure();
const connection = '11111111-1111-4111-8111-111111111111';
const entities = ['22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333'];
function session(): Session { return { version: 1, server: config.server, clientId: 'test-client', deviceKey: generateDeviceKey(), credentialEnrollmentRequested: true }; }
function envelope(s: Session) {
  const deadline = new Date(Date.now() + 300_000).toISOString();
  const expiry = new Date(Date.now() + 86400_000).toISOString();
  return { connection_id: connection, public_key: s.deviceKey.publicKey,
    public_key_fingerprint: createHash('sha256').update(Buffer.from(s.deviceKey.publicKey, 'hex')).digest('hex'),
    entity_ids: entities, state: 'awaiting_key_proof', enrollment_expires_at: deadline, credential_expires_at: expiry,
    proof_payload: JSON.stringify({ purpose: 'reah.mcp.credential-possession.v1', nonce: 'ab'.repeat(32),
      resource: config.server, connectionId: connection, userId: '44444444-4444-4444-8444-444444444444',
      clientId: s.clientId, publicKey: s.deviceKey.publicKey, enrollmentExpiresAt: deadline, credentialExpiresAt: expiry,
      targets: entities.map(entityId => ({ entityId, suborgId: `org-${entityId}`, authorizerTurnkeyUserId: 'human' })) }) };
}

test('possession proof signs exact UTF-8 payload, not base64 or an approval', () => {
  const s = session(); const raw = envelope(s); const e = parseEnrollment(raw, config, s);
  const sig = Buffer.from(signEnrollment(e, s), 'hex');
  const key = { key: createPublicKey(s.deviceKey.privateKey), dsaEncoding: 'ieee-p1363' as const };
  assert.equal(sig.length, 64);
  assert.ok(verify('sha256', Buffer.from(raw.proof_payload), key, sig));
  assert.equal(verify('sha256', Buffer.from(Buffer.from(raw.proof_payload).toString('base64')), key, sig), false);
  assert.throws(() => signEnrollment({ ...e, state: 'awaiting_provider' }, s));
});

test('proof rejects another server, client, key, connection, business, deadline or signing domain', () => {
  const s = session(); const base = envelope(s);
  for (const changes of [ { purpose: 'transfer' }, { resource: 'https://evil.example/mcp' },
    { clientId: 'other' }, { publicKey: generateDeviceKey().publicKey }, { connectionId: entities[0] },
    { targets: [{ entityId: connection, suborgId: 'org', authorizerTurnkeyUserId: 'human' }] },
    { credentialExpiresAt: new Date(Date.now() + 999_999_999).toISOString() }, { extra: 'request' } ]) {
    const raw = { ...base, proof_payload: JSON.stringify({ ...JSON.parse(base.proof_payload), ...changes }) };
    assert.throws(() => parseEnrollment(raw, config, s), /invalid or mismatched/);
  }
  assert.throws(() => parseEnrollment({ ...base, state: 'active' }, config, s));
  assert.throws(() => parseEnrollment({ ...base, enrollment_expires_at: new Date(0).toISOString() }, config, s));
  const previous = parseEnrollment(base, config, s);
  assert.throws(() => parseEnrollment({ ...base, proof_payload: base.proof_payload.replace('abab', 'cdcd') }, config, { ...s, enrollment: enrollmentReceipt(previous) }));
});

test('equivalent protobuf timestamp padding does not change the signed proof', () => {
  const s = session(); const raw = envelope(s);
  const deadline = new Date(Date.now() + 300_000).toISOString().replace(/\.\d+Z$/, '.120Z');
  raw.enrollment_expires_at = deadline;
  raw.proof_payload = JSON.stringify({ ...JSON.parse(raw.proof_payload), enrollmentExpiresAt: deadline.replace('.120Z', '.12Z') });
  assert.equal(parseEnrollment(raw, config, s).proofPayload, raw.proof_payload);
});

test('OAuth binds key before consent, signs locally, and safely resumes a lost proof response', async () => {
  let saved: Session | null = null;
  const store: Store = { read: async () => structuredClone(saved), write: async v => { saved = structuredClone(v); },
    remove: async () => { saved = null; }, exclusive: async action => action() };
  let offeredKey = ''; let raw: ReturnType<typeof envelope> | undefined;
  let proofs = 0; let loseProof = true; const callbacks: Promise<Response>[] = [];
  const requests: string[] = [];
  const json = (v: unknown) => new Response(JSON.stringify(v), { headers: { 'content-type': 'application/json' } });
  const mockFetch: typeof fetch = async (input, init) => {
    const url = new URL(String(input)); requests.push(String(init?.body ?? ''));
    switch (url.pathname) {
      case '/.well-known/oauth-protected-resource/mcp': return json({ resource: config.server, authorization_servers: [config.origin] });
      case '/.well-known/oauth-authorization-server': return json({ issuer: config.origin,
        ...Object.fromEntries(['authorization', 'token', 'revocation', 'registration'].map((k, i) => [`${k}_endpoint`, `${config.origin}/mcp/oauth/${['authorize', 'token', 'revoke', 'register'][i]}`])),
        code_challenge_methods_supported: ['S256'], scopes_supported: ['reah.read'], reah_credential_enrollment_supported: true });
      case '/mcp/oauth/register': return json({ client_id: 'test-client' });
      case '/mcp/oauth/token': return json({ access_token: 'test-access', refresh_token: 'test-refresh', token_type: 'Bearer', scope: 'reah.read', expires_in: 900 });
      case '/mcp/credentials/prepare':
        assert.equal(new Headers(init?.headers).get('authorization'), 'Bearer test-access');
        assert.deepEqual(JSON.parse(String(init?.body)), {});
        assert.equal(saved!.deviceKey.publicKey, offeredKey);
        raw ??= envelope(saved!); return json(raw);
      case '/mcp/credentials/prove': {
        proofs++; const { signature, ...extra } = JSON.parse(String(init?.body)); assert.deepEqual(extra, {});
        assert.equal(new Headers(init?.headers).get('authorization'), 'Bearer test-access');
        assert.ok(verify('sha256', Buffer.from(raw!.proof_payload), { key: createPublicKey(saved!.deviceKey.privateKey), dsaEncoding: 'ieee-p1363' }, Buffer.from(signature, 'hex')));
        raw!.state = 'awaiting_provider';
        if (loseProof) { loseProof = false; throw new Error('connection lost'); }
        return json(raw);
      }
      default: throw new Error('unexpected request');
    }
  };
  const oauth = new ReahOAuth(config, store, mockFetch);
  await assert.rejects(oauth.login(async href => {
    const u = new URL(href); offeredKey = u.searchParams.get('credential_public_key')!; assert.equal(offeredKey.length, 66);
    const cb = new URL(u.searchParams.get('redirect_uri')!); cb.search = new URLSearchParams({ state: u.searchParams.get('state')!, code: 'code' }).toString();
    callbacks.push(fetch(cb));
  }, 2000, true), /did not complete/);
  for (const callback of callbacks) assert.equal((await callback).status, 200, 'OAuth completed even if preparation was interrupted');
  await oauth.enrollKey();
  assert.equal(proofs, 1, 'completed proof is recovered without replaying it');
  assert.equal((await oauth.status()).signingKey?.enrollment, 'awaiting_provider');
  assert.doesNotMatch(requests.join(''), /PRIVATE KEY/);
  assert.doesNotMatch(JSON.stringify(await oauth.status()), /PRIVATE KEY|test-access|test-refresh/);
});

test('unsupported server refuses opt-in before changing existing credentials', async () => {
  const s = session(); let writes = 0;
  const store: Store = { read: async () => s, write: async () => { writes++; }, remove: async () => {}, exclusive: async action => action() };
  const oauth = new ReahOAuth(config, store, async input => new Response(JSON.stringify(String(input).includes('protected-resource')
    ? { resource: config.server, authorization_servers: [config.origin] }
    : { issuer: config.origin, code_challenge_methods_supported: ['S256'], scopes_supported: ['reah.read'] })));
  await assert.rejects(oauth.login(async () => assert.fail('must not open browser'), 2000, true), /does not support/);
  assert.equal(writes, 0);
});


test('OS vault stores a bounded receipt even for 100 authorized businesses', () => {
  const s = session(); const raw = envelope(s);
  const ids = Array.from({ length: 100 }, (_, i) => `55555555-5555-4555-8555-${i.toString().padStart(12, '0')}`);
  raw.entity_ids = ids;
  raw.proof_payload = JSON.stringify({ ...JSON.parse(raw.proof_payload), targets: ids.map(entityId => ({
    entityId, suborgId: '66666666-6666-4666-8666-666666666666', authorizerTurnkeyUserId: '77777777-7777-4777-8777-777777777777' })) });
  s.clientId = 'reah_mcp_client_' + 'a'.repeat(43);
  raw.proof_payload = JSON.stringify({ ...JSON.parse(raw.proof_payload), clientId: s.clientId });
  const e = parseEnrollment(raw, config, s);
  const stored = { ...s, enrollment: enrollmentReceipt(e), tokens: {
    accessToken: 'reah_mcp_at_' + 'a'.repeat(43), refreshToken: 'reah_mcp_rt_' + 'b'.repeat(43), expiresAt: Date.now() + 900_000 } };
  assert.ok(Buffer.byteLength(JSON.stringify(stored), 'utf16le') < 2560);
  assert.doesNotMatch(JSON.stringify(stored.enrollment), /targets|entityIds|proofPayload/);
  assert.doesNotThrow(() => parseEnrollment(raw, config, stored));
  raw.proof_payload = raw.proof_payload.replace(ids[0]!, connection);
  assert.throws(() => parseEnrollment(raw, config, stored));
});
