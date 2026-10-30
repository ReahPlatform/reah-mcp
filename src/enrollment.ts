import { createHash, sign } from 'node:crypto';
import type { Config } from './config.js';
import { LocalError } from './config.js';
import type { Session } from './store.js';

export interface Enrollment {
  connectionId: string; publicKey: string; entityIds: string[];
  state: 'awaiting_key_proof' | 'awaiting_provider' | 'expired' | 'revoked';
  proofPayload: string; enrollmentExpiresAt: string; credentialExpiresAt: string;
}
const uuid = (v: unknown): v is string => typeof v === 'string' &&
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v) && v !== '00000000-0000-0000-0000-000000000000';
const obj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const text = (v: unknown): v is string => typeof v === 'string' && v.length > 0 && v.length <= 512;
const timestamp = (value: string) => value.replace(/(\.\d*?[1-9])0+Z$/, '$1Z').replace(/\.0+Z$/, 'Z');
function invalid(): never { throw new LocalError('Reah returned an invalid or mismatched credential proof. No signature was sent.'); }

// Accept only the fixed Hodor possession-proof domain. Never sign arbitrary
// server messages, tool arguments, Web approvals, or Turnkey request bodies.
export function parseEnrollment(value: Record<string, unknown>, config: Config, session: Session): Enrollment {
  const ids = value.entity_ids;
  const state = value.state;
  if (!uuid(value.connection_id) || value.public_key !== session.deviceKey.publicKey ||
      value.public_key_fingerprint !== createHash('sha256').update(Buffer.from(session.deviceKey.publicKey, 'hex')).digest('hex') ||
      !Array.isArray(ids) || ids.length < 1 || ids.length > 100 || !ids.every(uuid) ||
      ids.some((id, i) => i > 0 && id <= ids[i - 1]!) ||
      !['awaiting_key_proof', 'awaiting_provider', 'expired', 'revoked'].includes(state as string) ||
      typeof value.proof_payload !== 'string' || value.proof_payload.length > 65_536 ||
      typeof value.enrollment_expires_at !== 'string' || typeof value.credential_expires_at !== 'string') invalid();
  const deadline = Date.parse(value.enrollment_expires_at), expiry = Date.parse(value.credential_expires_at);
  if (!Number.isFinite(deadline) || !Number.isFinite(expiry) || deadline > expiry) invalid();
  if (session.enrollment && (session.enrollment.connectionId !== value.connection_id ||
      session.enrollment.credentialExpiresAt !== value.credential_expires_at)) invalid();
  if (state === 'awaiting_key_proof' || state === 'awaiting_provider') {
    if (deadline <= Date.now() || deadline > Date.now() + 610_000 || expiry > Date.now() + 30 * 86400_000) invalid();
    let p: unknown;
    try { p = JSON.parse(value.proof_payload); } catch { invalid(); }
    if (!obj(p) || Object.keys(p).sort().join(',') !==
        'clientId,connectionId,credentialExpiresAt,enrollmentExpiresAt,nonce,publicKey,purpose,resource,targets,userId' ||
        p.purpose !== 'reah.mcp.credential-possession.v1' || p.publicKey !== session.deviceKey.publicKey ||
        p.resource !== config.server || p.clientId !== session.clientId || p.connectionId !== value.connection_id ||
        !uuid(p.userId) || typeof p.nonce !== 'string' || !/^[0-9a-f]{64}$/.test(p.nonce) ||
        typeof p.enrollmentExpiresAt !== 'string' || timestamp(p.enrollmentExpiresAt) !== timestamp(value.enrollment_expires_at) ||
        typeof p.credentialExpiresAt !== 'string' || timestamp(p.credentialExpiresAt) !== timestamp(value.credential_expires_at) ||
        !Array.isArray(p.targets) || p.targets.length !== ids.length ||
        p.targets.some((t, i) => !obj(t) || Object.keys(t).sort().join(',') !== 'authorizerTurnkeyUserId,entityId,suborgId' ||
          t.entityId !== ids[i] || !text(t.suborgId) || !text(t.authorizerTurnkeyUserId))) invalid();
    if (session.enrollment && session.enrollment.proofHash !== createHash('sha256').update(value.proof_payload).digest('hex')) invalid();
  } else if (value.proof_payload !== '') invalid();
  return { connectionId: value.connection_id, publicKey: session.deviceKey.publicKey, entityIds: ids,
    state: state as Enrollment['state'], proofPayload: value.proof_payload,
    enrollmentExpiresAt: value.enrollment_expires_at, credentialExpiresAt: value.credential_expires_at };
}

export function signEnrollment(enrollment: Enrollment, session: Session): string {
  if (enrollment.state !== 'awaiting_key_proof' || enrollment.publicKey !== session.deviceKey.publicKey ||
      Date.parse(enrollment.enrollmentExpiresAt) <= Date.now()) invalid();
  return sign('sha256', Buffer.from(enrollment.proofPayload, 'utf8'), {
    key: session.deviceKey.privateKey, dsaEncoding: 'ieee-p1363',
  }).toString('hex');
}

// Keep only a compact receipt in the OS vault. Full multi-entity proofs can
// exceed Windows Credential Manager's 2560-byte UTF-16 password limit.
export interface EnrollmentReceipt {
  connectionId: string; state: Enrollment['state']; credentialExpiresAt: string; proofHash: string;
}
export function enrollmentReceipt(enrollment: Enrollment): EnrollmentReceipt {
  return { connectionId: enrollment.connectionId, state: enrollment.state,
    credentialExpiresAt: enrollment.credentialExpiresAt,
    proofHash: createHash('sha256').update(enrollment.proofPayload).digest('hex') };
}
