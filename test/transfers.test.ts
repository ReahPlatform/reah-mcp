import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createPublicKey, sign, verify } from 'node:crypto';
import { fromBinary } from '@bufbuild/protobuf';
import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';
import { SignedApprovalIntentSchema } from '../src/generated/approval_details_pb.js';
import { generateDeviceKey } from '../src/signing.js';
import { callFinancialTool, executionStamp, operationProof, transferIntent, validateSigningContext, type SigningContext } from '../src/transfers.js';
import { reviewedTools } from '../src/proxy.js';

const id = '11111111-1111-4111-8111-111111111111';
const other = '22222222-2222-4222-8222-222222222222';
const key = generateDeviceKey();
const context: SigningContext = {userId:id,connectionId:other,resource:'https://platform.stg.reah.com/mcp',binding:{entityId:id,publicKey:key.publicKey,suborgId:other,expiresAt:new Date(Date.now()+86400_000).toISOString()}};
const args = {entityId:id,requestId:other,instrumentAccountId:id,walletId:id,networkId:id,instrumentId:id,fromAddress:'0x1111111111111111111111111111111111111111',toAddress:'0x2222222222222222222222222222222222222222',amount:'900719925474099300000001',asset:'eip155:1/erc20:0x3333333333333333333333333333333333333333',gasSource:'GAS_SOURCE_FROM_WALLET'};
const estimateOK = (input: Record<string,unknown>) => ok({transfer:input,assetBalance:{sufficient:true},gasOptions:[{gasSource:'GAS_SOURCE_FROM_WALLET',sufficient:true},{gasSource:'GAS_SOURCE_GAS_STATION',sufficient:true}]});
const ok = (value: Record<string,unknown>): CallToolResult => ({content:[{type:'text',text:JSON.stringify(value)}],structuredContent:value});

test('transfer uses the Web protobuf contract with exact source, recipient and integer amount', () => {
 const signed=transferIntent(key,context,args);
 assert.ok(verify('sha256',Buffer.from(signed.payloadBytes),{key:createPublicKey(key.privateKey),dsaEncoding:'ieee-p1363'},Buffer.from(signed.signature,'hex')));
 const intent=fromBinary(SignedApprovalIntentSchema,Buffer.from(signed.payloadBytes,'base64'));
 assert.equal(intent.domain,'reah.approval_intent.v1');
 assert.equal(intent.entityId,id); assert.equal(intent.initiatorUserId,id); assert.equal(intent.nonce,args.requestId);
 assert.equal(intent.details?.kind.case,'sendTransaction');
 if (intent.details?.kind.case!=='sendTransaction') throw new Error('wrong intent');
 const send=intent.details.kind.value;
 assert.equal(send.amount,args.amount); assert.equal(send.fromAddress,args.fromAddress); assert.equal(send.toAddress,args.toAddress);
 assert.equal(send.fromTo?.from[0]?.amount,args.amount); assert.equal(send.fromTo?.to[0]?.address,args.toAddress);
 assert.equal(send.instrumentAccountId,args.instrumentAccountId); assert.equal(send.v,2);
 assert.throws(()=>transferIntent(key,context,{...args,amount:'1.5'}));
 assert.throws(()=>transferIntent(key,context,{...args,requestSignature:{}}));
});

test('operation proof covers connection, tool, entity and every argument', () => {
 const proof=operationProof(key,context,'reah_transfer',args);
 const message=JSON.parse(proof.payload);
 assert.equal(message.connectionId,other); assert.equal(message.tool,'reah_transfer'); assert.deepEqual(message.arguments,args);
 assert.ok(verify('sha256',Buffer.from(proof.payload),{key:createPublicKey(key.privateKey),dsaEncoding:'ieee-p1363'},Buffer.from(proof.signature,'hex')));
 assert.ok(!verify('sha256',Buffer.from(proof.payload.replace(args.amount,'1')),{key:createPublicKey(key.privateKey),dsaEncoding:'ieee-p1363'},Buffer.from(proof.signature,'hex')));
 assert.throws(()=>validateSigningContext({...context,binding:{...context.binding,publicKey:generateDeviceKey().publicKey}},context.resource,id,key));
});

test('execution stamps require an authentic server request for the bound Turnkey organization', () => {
 const serverKey=generateDeviceKey(); const publicKey=createPublicKey(serverKey.privateKey).export({type:'spki',format:'pem'}).toString();
 const requestPayload=JSON.stringify({organizationId:other,type:'ACTIVITY_TYPE_SIGN_RAW_PAYLOADS',timestampMs:String(Date.now()),parameters:{signWith:args.fromAddress,payloads:['abcd'],encoding:'PAYLOAD_ENCODING_HEXADECIMAL',hashFunction:'HASH_FUNCTION_NO_OP'}});
 const signature=sign('sha256',Buffer.from(requestPayload),serverKey.privateKey).toString('base64');
 const request={requestId:other,requestPayload,signature};
 const signed=executionStamp(key,context,request,publicKey);
 const stamp=JSON.parse(Buffer.from(signed.signature,'base64url').toString());
 assert.equal(stamp.publicKey,key.publicKey); assert.equal(stamp.scheme,'SIGNATURE_SCHEME_TK_API_P256');
 assert.ok(verify('sha256',Buffer.from(requestPayload),createPublicKey(key.privateKey),Buffer.from(stamp.signature,'hex')));
 assert.throws(()=>executionStamp(key,context,{...request,requestPayload:requestPayload.replace(other,id)},publicKey));
 assert.throws(()=>executionStamp(key,{...context,binding:{...context.binding,suborgId:id}},request,publicKey));
 const dangerous=JSON.stringify({organizationId:other,type:'ACTIVITY_TYPE_EXPORT_WALLET',timestampMs:String(Date.now()),parameters:{signWith:args.fromAddress}});
 assert.throws(()=>executionStamp(key,context,{requestId:other,requestPayload:dangerous,signature:sign('sha256',Buffer.from(dangerous),serverKey.privateKey).toString('base64')},publicKey));
});

test('transfer invokes context then one signed mutation and preserves approval outcome', async () => {
 const calls: Array<{name:string,args:Record<string,unknown>}> = [];
 const result = await callFinancialTool('reah_transfer',args,key,context.resource,async (name,input)=>{
  calls.push({name,args:input});
  if(name==='reah_signing_context') return ok(context as unknown as Record<string,unknown>);
  if(name==='reah_estimate_transfer') return estimateOK(input);
  const proof=JSON.parse((input._proof as {payload:string}).payload);
  const {_proof,...unsigned}=input; assert.deepEqual(proof.arguments,unsigned);
  assert.equal(input.requestId,args.requestId); assert.ok(input.requestSignature);
  return ok({success:true,approvalId:other});
 });
 assert.equal(result.structuredContent?.approvalId,other); assert.equal(calls.length,3);
});

test('ambiguous financial requests are never retried automatically', async () => {
 let mutations=0;
 await assert.rejects(callFinancialTool('reah_transfer',args,key,context.resource,async (name,input)=>{
  if(name==='reah_signing_context') return ok(context as unknown as Record<string,unknown>);
  if(name==='reah_estimate_transfer') return estimateOK(input);
  mutations++; throw new Error('network connection lost');
 }),/outcome could not be confirmed/);
 assert.equal(mutations,1);
});

test('read-only or unregistered credentials cannot reach a financial mutation', async () => {
 let calls=0;
 const result=await callFinancialTool('reah_transfer',args,key,context.resource,async name=>{
  calls++; assert.equal(name,'reah_signing_context'); return {...ok({error:{code:'CREDENTIAL_REQUIRED'}}),isError:true};
 });
 assert.equal(result.isError,true); assert.equal(calls,1);
});

test('model-visible financial tools never accept raw signing material', () => {
 const [tool]=reviewedTools([{name:'reah_execution',inputSchema:{type:'object',properties:{entityId:{type:'string'},approvalId:{type:'string'},_proof:{type:'object'},signatures:{type:'array'}},required:['entityId','approvalId','_proof']},annotations:{readOnlyHint:false}}]);
 assert.ok(tool); assert.ok(!('_proof' in tool.inputSchema.properties!)); assert.ok(!('signatures' in tool.inputSchema.properties!)); assert.deepEqual(tool.inputSchema.required,['entityId','approvalId']);
 assert.deepEqual(reviewedTools([{name:'sign_anything',inputSchema:{type:'object'},annotations:{readOnlyHint:false}}]),[]);
});

// Only the provider signing key is substituted; proof generation, protobuf,
// server-payload verification and the two-stage protocol are production code.
test('execution completes the authenticated two-stage protocol without exposing stamps', async () => {
 const serverKey=generateDeviceKey();
 const publicKey=createPublicKey(serverKey.privateKey).export({type:'spki',format:'pem'}).toString();
 const requestPayload=JSON.stringify({organizationId:other,type:'ACTIVITY_TYPE_SIGN_RAW_PAYLOADS',timestampMs:String(Date.now()),parameters:{signWith:args.fromAddress,payloads:['abcd']}});
 const request={requestId:other,requestPayload,signature:sign('sha256',Buffer.from(requestPayload),serverKey.privateKey).toString('base64')};
 let step=0;
 const result=await callFinancialTool('reah_execution',{entityId:id,approvalId:other},key,context.resource,async (name,input)=>{
  if(name==='reah_signing_context') return ok(context as unknown as Record<string,unknown>);
  assert.equal(name,'reah_execution'); assert.equal(input.approvalId,other);
  const proof=input._proof as {payload:string,signature:string};
  const {_proof,...unsigned}=input;
  assert.deepEqual(JSON.parse(proof.payload).arguments,unsigned);
  assert.ok(verify('sha256',Buffer.from(proof.payload),{key:createPublicKey(key.privateKey),dsaEncoding:'ieee-p1363'},Buffer.from(proof.signature,'hex')));
  step++;
  if(step===1) { assert.equal(input.signatures,undefined); return ok({success:false,requiresSignatures:[request]}); }
  assert.equal(step,2);
  const stamps=input.signatures as Array<{requestId:string,signature:string}>;
  assert.equal(stamps.length,1); assert.equal(stamps[0]?.requestId,other);
  const stamp=JSON.parse(Buffer.from(stamps[0]!.signature,'base64url').toString());
  assert.ok(verify('sha256',Buffer.from(requestPayload),createPublicKey(key.privateKey),Buffer.from(stamp.signature,'hex')));
  return ok({success:true,requiresSignatures:[]});
 },undefined,publicKey);
 assert.equal(step,2); assert.equal(result.structuredContent?.success,true);
 assert.ok(!JSON.stringify(result).includes(key.privateKey));
});

test('an invalid server execution signature stops before the continuation', async () => {
 let calls=0;
 await assert.rejects(callFinancialTool('reah_execution',{entityId:id,approvalId:other},key,context.resource,async name=>{
  if(name==='reah_signing_context') return ok(context as unknown as Record<string,unknown>);
  calls++; return ok({success:false,requiresSignatures:[{requestId:other,requestPayload:'{}',signature:'invalid'}]});
 }),/signature could not be verified/);
 assert.equal(calls,1);
});

test('gas payer is explicit and covered by the signed intent', () => {
 for (const gasSource of ['GAS_SOURCE_FROM_WALLET','GAS_SOURCE_GAS_STATION']) {
  const signed = transferIntent(key,context,{...args,gasSource});
  const intent=fromBinary(SignedApprovalIntentSchema,Buffer.from(signed.payloadBytes,'base64'));
  assert.equal(intent.details?.kind.case,'sendTransaction');
  if (intent.details?.kind.case !== 'sendTransaction') throw new Error('wrong intent');
  assert.equal(intent.details.kind.value.gasSource,gasSource);
  assert.ok(verify('sha256',Buffer.from(signed.payloadBytes),{key:createPublicKey(key.privateKey),dsaEncoding:'ieee-p1363'},Buffer.from(signed.signature,'hex')));
 }
 for (const gasSource of ['GAS_SOURCE_FROM_BANKING','GAS_SOURCE_UNSPECIFIED','']) assert.throws(()=>transferIntent(key,context,{...args,gasSource}));
});


test('transfer stops before signing submission when asset or selected gas balance is insufficient', async () => {
 for (const failure of ['asset','wallet','station','missing','mismatch']) {
  let mutations=0;
  await assert.rejects(callFinancialTool('reah_transfer',{...args,gasSource:failure==='station'?'GAS_SOURCE_GAS_STATION':'GAS_SOURCE_FROM_WALLET'},key,context.resource,async (name,input)=>{
   if(name==='reah_signing_context') return ok(context as unknown as Record<string,unknown>);
   if(name==='reah_estimate_transfer') return ok({transfer:failure==='mismatch'?{...input,amount:'1'}:input,assetBalance:{sufficient:failure!=='asset'},gasOptions:failure==='missing'?[]:[{gasSource:'GAS_SOURCE_FROM_WALLET',sufficient:failure!=='wallet'},{gasSource:'GAS_SOURCE_GAS_STATION',sufficient:failure!=='station'}]});
   mutations++; return ok({success:true});
  }));
  assert.equal(mutations,0);
 }
});
