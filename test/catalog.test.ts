import assert from 'node:assert/strict';
import { test } from 'node:test';
import type { Tool } from '@modelcontextprotocol/sdk/types.js';
import { LOCAL_PROTOCOL_META, publicTools } from '../src/catalog.js';

const tool = (name: string, readOnlyHint: boolean, protocol?: unknown): Tool => ({
  name, description: 'Authoritative server instructions.',
  inputSchema: { type: 'object', properties: { entityId: { type: 'string' } }, required: ['entityId'] },
  annotations: { readOnlyHint },
  ...(protocol === undefined ? {} : { _meta: { [LOCAL_PROTOCOL_META]: protocol } }),
});

test('new business names and schemas follow the authenticated server without a release', () => {
  const read = tool('reah_new_report',true);
  const preparation = tool('reah_new_preparation',false,{version:1,mode:'proxy'});
  assert.deepEqual(publicTools([read,preparation]),[read,preparation]);
  for (const signing of ['approval-send-v2','turnkey-execution-v1']) {
    const signed = tool('reah_future_operation',false,{version:1,mode:'signed',signing});
    signed.inputSchema.properties = { ...signed.inputSchema.properties, _proof: {}, requestSignature: {}, signatures: {} };
    signed.inputSchema.required!.push('_proof');
    const [visible] = publicTools([signed]);
    assert.equal(visible?.name,signed.name);
    assert.equal(visible?.description,signed.description);
    assert.deepEqual(visible?.inputSchema.required,['entityId']);
    assert.deepEqual(Object.keys(visible!.inputSchema.properties!),['entityId']);
    assert.ok('_proof' in signed.inputSchema.properties!, 'do not mutate the upstream schema');
  }
});

test('unknown protocol versions or formats never downgrade to unsigned calls', () => {
  for (const protocol of [null, 'signed', [], {}, {version:2,mode:'proxy'},
    {version:1,mode:'unknown'}, {version:1,mode:'signed'},
    {version:1,mode:'signed',signing:'arbitrary-bytes'}]) {
    assert.deepEqual(publicTools([tool('future_read',true,protocol),tool('future_write',false,protocol)]),[]);
  }
  assert.deepEqual(publicTools([tool('internal_helper',true,{version:1,mode:'internal'}),tool('reah_signing_context',true)]),[]);
  assert.deepEqual(publicTools([tool('unsigned_write',false)]),[]);
  assert.deepEqual(publicTools([tool('bad_annotation',true,{version:1,mode:'signed',signing:'approval-send-v2'})]),[]);
});

test('legacy or proxy tools with signing fields cannot expose raw signing inputs', () => {
  for (const protocol of [undefined,{version:1,mode:'proxy'}]) {
    for (const field of ['_proof','requestSignature','signatures']) {
      const unsafe = tool('legacy_signed_tool',true,protocol);
      unsafe.inputSchema.properties![field] = {};
      assert.deepEqual(publicTools([unsafe]),[]);
    }
  }
});
