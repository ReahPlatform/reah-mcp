import { randomUUID, sign, verify } from 'node:crypto';
import { create, toBinary } from '@bufbuild/protobuf';
import { timestampFromDate } from '@bufbuild/protobuf/wkt';
import { ApprovalDetailsSchema, BalanceSource, FromToSchema, Party_PartyType, PartySchema,
  SendTransactionDetailsSchema, SignedApprovalIntentSchema } from './generated/approval_details_pb.js';
import { LocalError } from './config.js';
import { signApprovalPayload, type DeviceKey } from './signing.js';
import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';

export const WRITE_TOOLS = new Set(['reah_transfer', 'reah_execution']);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const amount = /^[1-9][0-9]{0,77}$/;
const stagingKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEvtIyoor6aZLs9QVQ1Cj5bekX28hL
QpIRvUwuKaxV7lIfxTxRfYTX0YE10XgwELydE9spUeUwO34xEEXKBifXjA==
-----END PUBLIC KEY-----`;
const productionKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEP0tIL+DaFftn17dRB87c19wbf5QV
4NYTeww6cXvSILTf3rFRvflz6EApe9mGA2+6v/6lD7dLhVAdcej3kSyQrg==
-----END PUBLIC KEY-----`;

export interface SigningContext {
 userId: string; connectionId: string; resource: string;
 binding: { entityId: string; publicKey: string; suborgId: string; expiresAt: string };
}
type Invoke = (name: string, args: Record<string, unknown>, signal?: AbortSignal) => Promise<CallToolResult>;

function object(value: unknown): Record<string, unknown> {
 if (!value || typeof value !== 'object' || Array.isArray(value)) throw new LocalError('Invalid Reah signing response.');
 return value as Record<string, unknown>;
}
function resultData(result: CallToolResult): Record<string, unknown> {
 if (result.isError) throw new LocalError('Reah could not authorize this operation. Check the connection and original approval.');
 return object(result.structuredContent);
}

export function validateSigningContext(value: unknown, server: string, entityId: string, key: DeviceKey): SigningContext {
 const context = object(value), binding = object(context.binding);
 if (context.resource !== server || typeof context.userId !== 'string' || !uuid.test(context.userId) ||
     typeof context.connectionId !== 'string' || !uuid.test(context.connectionId) || binding.entityId !== entityId ||
     binding.publicKey !== key.publicKey || typeof binding.suborgId !== 'string' || !uuid.test(binding.suborgId) ||
     typeof binding.expiresAt !== 'string' || !Number.isFinite(Date.parse(binding.expiresAt)) || Date.parse(binding.expiresAt) <= Date.now()) {
  throw new LocalError('This local key is not authorized to transfer for the selected business. Complete credential registration in Reah.');
 }
 return context as unknown as SigningContext;
}

export function operationProof(key: DeviceKey, context: SigningContext, tool: string, args: Record<string, unknown>) {
 if (!WRITE_TOOLS.has(tool)) throw new LocalError('Unsupported signing operation.');
 const payload = JSON.stringify({ domain: 'reah.mcp.operation.v1', resource: context.resource, connectionId: context.connectionId,
  tool, entityId: context.binding.entityId, nonce: randomUUID(), expiresAt: Math.floor(Date.now()/1000)+60, arguments: args });
 return { payload, signature: sign('sha256', Buffer.from(payload), { key: key.privateKey, dsaEncoding: 'ieee-p1363' }).toString('hex') };
}

const transferFields = ['entityId','requestId','instrumentAccountId','walletId','networkId','instrumentId','fromAddress','toAddress','amount','asset','gasSource','memo','notes'];
export function validateTransferArgs(args: Record<string, unknown>): void {
 if (Object.keys(args).some(key => !transferFields.includes(key)) || Object.values(args).some(value => typeof value !== 'string')) throw new LocalError('Use the advertised transfer arguments; signatures are generated locally.');
 for (const field of transferFields.filter(field => !['memo','notes'].includes(field))) if (typeof args[field] !== 'string' || !args[field]) throw new LocalError(`Missing transfer field: ${field}.`);
 for (const field of ['entityId','requestId','instrumentAccountId','walletId','networkId','instrumentId']) if (!uuid.test(args[field] as string) || args[field] === '00000000-0000-0000-0000-000000000000') throw new LocalError(`Invalid transfer UUID: ${field}.`);
 if (!amount.test(args.amount as string) || !['GAS_SOURCE_FROM_WALLET','GAS_SOURCE_GAS_STATION'].includes(args.gasSource as string) || String(args.memo ?? '').length > 1024 || String(args.notes ?? '').length > 4096) throw new LocalError('Use a positive integer amount in asset minor units and explicitly choose wallet or Gas Station gas payment.');
}

export function transferIntent(key: DeviceKey, context: SigningContext, args: Record<string, unknown>, now = new Date()) {
 validateTransferArgs(args);
 const a = args as Record<string, string>;
 const party = (address: string) => create(PartySchema, { partyType: Party_PartyType.ADDRESS, address, instrumentId: a.instrumentId!, amount: a.amount! });
 const details = create(SendTransactionDetailsSchema, { v: 2, instrumentAccountId: a.instrumentAccountId!, walletId: a.walletId!,
  networkId: a.networkId!, instrumentId: a.instrumentId!, fromAddress: a.fromAddress!, toAddress: a.toAddress!, amount: a.amount!, asset: a.asset!,
  memo: a.memo ?? '', notes: a.notes ?? '', gasSource: a.gasSource!, balanceSource: BalanceSource.WALLET,
  fromTo: create(FromToSchema, { from: [party(a.fromAddress!)], to: [party(a.toAddress!)] }) });
 const expires = new Date(Math.min(now.getTime()+7*86400_000, Date.parse(context.binding.expiresAt)));
 const intent = create(SignedApprovalIntentSchema, { domain:'reah.approval_intent.v1', entityId:context.binding.entityId,
  initiatorUserId:context.userId, nonce:a.requestId!, issuedAt:timestampFromDate(now), expiresAt:timestampFromDate(expires),
  details:create(ApprovalDetailsSchema, { kind:{case:'sendTransaction',value:details} }) });
 return signApprovalPayload(key, Buffer.from(toBinary(SignedApprovalIntentSchema,intent)).toString('base64'));
}

export function executionStamp(key: DeviceKey, context: SigningContext, request: unknown, publicKey?: string) {
 const item = object(request);
 if (typeof item.requestId !== 'string' || !item.requestId || typeof item.requestPayload !== 'string' || item.requestPayload.length > 128*1024 ||
     typeof item.signature !== 'string' || !item.signature) throw new LocalError('Invalid execution signing request.');
 const trustedKey = publicKey ?? (context.resource === 'https://platform.stg.reah.com/mcp' ? stagingKey : context.resource === 'https://platform.reah.com/mcp' ? productionKey : undefined);
 if (!trustedKey) throw new LocalError('No trusted execution verification key is configured for this Reah environment.');
 // Same contract as reah-web: Hodor signs UTF-8 request JSON using P-256/SHA-256,
 // DER signature encoded as base64. Never sign a model-supplied provider body.
 if (!verify('sha256', Buffer.from(item.requestPayload), trustedKey, Buffer.from(item.signature,'base64'))) throw new LocalError('The execution request signature could not be verified.');
 const body = object(JSON.parse(item.requestPayload));
 const parameters = object(body.parameters);
 const timestamp = Number(body.timestampMs);
 if (body.organizationId !== context.binding.suborgId || !['ACTIVITY_TYPE_SIGN_RAW_PAYLOADS','ACTIVITY_TYPE_SIGN_TRANSACTION_V2'].includes(String(body.type)) ||
     !Number.isSafeInteger(timestamp) || timestamp > Date.now()+30_000 || timestamp < Date.now()-10*60_000 || typeof parameters.signWith !== 'string' || !parameters.signWith) {
  throw new LocalError('The execution request targets an unexpected organization or signing operation.');
 }
 const signature = sign('sha256',Buffer.from(item.requestPayload),{key:key.privateKey,dsaEncoding:'der'}).toString('hex');
 const stamp = Buffer.from(JSON.stringify({publicKey:key.publicKey,scheme:'SIGNATURE_SCHEME_TK_API_P256',signature})).toString('base64url');
 return { requestId:item.requestId, signature:stamp };
}

export async function callFinancialTool(name: string, args: Record<string, unknown>, key: DeviceKey, server: string, invoke: Invoke, signal?: AbortSignal, verificationKey?: string): Promise<CallToolResult> {
 if (!WRITE_TOOLS.has(name)) throw new LocalError('Unsupported financial tool.');
 if (name === 'reah_transfer') validateTransferArgs(args);
 else if (Object.keys(args).some(key => !['entityId','approvalId'].includes(key)) || typeof args.approvalId !== 'string' || !uuid.test(args.approvalId)) throw new LocalError('Execution requires entityId and the original approvalId; do not supply signatures.');
 if (typeof args.entityId !== 'string' || !uuid.test(args.entityId)) throw new LocalError('An authorized entityId is required.');
 const contextResult = await invoke('reah_signing_context',{entityId:args.entityId},signal);
 if (contextResult.isError) return contextResult;
 const context = validateSigningContext(resultData(contextResult),server,args.entityId,key);
 let submitted = false;
 const submit = async (input: Record<string, unknown>) => {
  const proof=operationProof(key,context,name,input);
  submitted=true;
  return invoke(name,{...input,_proof:proof},signal);
 };
 try {
  if (name === 'reah_transfer') {
   const estimateArgs = Object.fromEntries(['entityId','walletId','networkId','instrumentId','toAddress','amount'].map(field=>[field,args[field]]));
   const estimate = await invoke('reah_estimate_transfer',estimateArgs,signal);
   if (estimate.isError) return estimate;
   const data = resultData(estimate);
   const quoted = object(data.transfer);
   if (Object.entries(estimateArgs).some(([field,value])=>quoted[field]!==value)) throw new LocalError('The balance estimate does not match this transfer.');
   const balance = object(data.assetBalance);
   const gas = Array.isArray(data.gasOptions) ? data.gasOptions.find(item=>item && typeof item==='object' && item.gasSource===args.gasSource) : undefined;
   if (balance.sufficient !== true || !gas || gas.sufficient !== true) throw new LocalError('The selected wallet cannot cover the transfer and gas, or the balance check is unavailable. Do not switch network or gas payer automatically.');
   return await submit({...args,requestSignature:transferIntent(key,context,args)});
  }
  const result = await submit(args);
  if (result.isError) return result;
  const data = resultData(result);
  if (!Array.isArray(data.requiresSignatures) || data.requiresSignatures.length === 0) return result;
  if (data.requiresSignatures.length > 16) throw new LocalError('Reah requested too many execution signatures.');
  const signatures = data.requiresSignatures.map(item => executionStamp(key,context,item,verificationKey));
  if (new Set(signatures.map(item => item.requestId)).size !== signatures.length) throw new LocalError('Reah returned duplicate execution signing requests.');
  // This is the protocol's signature continuation, not a retry. Never repeat a
  // failed/ambiguous submission or create another transfer automatically.
  return await submit({...args,signatures});
 } catch (error) {
  if (error instanceof LocalError) throw error;
  if (!submitted) throw new LocalError('The balance check could not be completed. No transfer was submitted.');
  throw new LocalError('The transfer outcome could not be confirmed. Check the original requestId or approvalId before retrying. Do not create a replacement transfer.');
 }
}
