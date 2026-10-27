import { createPublicKey, ECDH, generateKeyPairSync, sign } from 'node:crypto';

export interface DeviceKey { privateKey: string; publicKey: string }

export function generateDeviceKey(): DeviceKey {
  const { privateKey, publicKey } = generateKeyPairSync('ec', { namedCurve: 'prime256v1' });
  const jwk = publicKey.export({ format: 'jwk' });
  const point = Buffer.concat([Buffer.from([4]), Buffer.from(jwk.x!, 'base64url'), Buffer.from(jwk.y!, 'base64url')]);
  return {
    privateKey: privateKey.export({ type: 'pkcs8', format: 'pem' }).toString(),
    publicKey: ECDH.convertKey(point, 'prime256v1', undefined, undefined, 'compressed').toString('hex'),
  };
}

// Web approval signatures cover the UTF-8 base64 protobuf text, not decoded bytes.
// This primitive does not enroll the key or authorize an operation.
export function signApprovalPayload(key: DeviceKey, payloadBytes: string) {
  if (!payloadBytes || Buffer.from(payloadBytes, 'base64').toString('base64') !== payloadBytes) {
    throw new Error('Expected canonical base64 approval bytes.');
  }
  const actual = createPublicKey(key.privateKey).export({ format: 'jwk' });
  const point = Buffer.concat([Buffer.from([4]), Buffer.from(actual.x!, 'base64url'), Buffer.from(actual.y!, 'base64url')]);
  if (ECDH.convertKey(point, 'prime256v1', undefined, undefined, 'compressed').toString('hex') !== key.publicKey) {
    throw new Error('Device key does not match its public key.');
  }
  return { payloadBytes, publicKey: key.publicKey,
    signature: sign('sha256', Buffer.from(payloadBytes, 'utf8'), {
      key: key.privateKey, dsaEncoding: 'ieee-p1363',
    }).toString('hex') };
}
