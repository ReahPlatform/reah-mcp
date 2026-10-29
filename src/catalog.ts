import type { Tool } from '@modelcontextprotocol/sdk/types.js';
import { LocalError } from './config.js';

export const LOCAL_PROTOCOL_META = 'reah.com/local';
export type SigningProtocol = 'approval-send-v2' | 'turnkey-execution-v1';
export const SIGNATURE_FIELDS = ['_proof', 'requestSignature', 'signatures'] as const;
type LocalProtocol = { version: 1; mode: 'proxy' | 'internal' } |
  { version: 1; mode: 'signed'; signing: SigningProtocol };

// The authenticated Gateway owns the business catalog. The client only knows
// supported signing formats, never a list of business tool names.
export function localProtocol(tool: Tool): LocalProtocol | undefined {
  const value = tool._meta?.[LOCAL_PROTOCOL_META];
  if (value === undefined) return undefined;
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  const meta = value as Record<string, unknown>;
  if (meta.version !== 1) return undefined;
  if (meta.mode === 'internal' || meta.mode === 'proxy') return { version: 1, mode: meta.mode };
  if (meta.mode === 'signed' && (meta.signing === 'approval-send-v2' || meta.signing === 'turnkey-execution-v1')) {
    return { version: 1, mode: 'signed', signing: meta.signing };
  }
  return undefined;
}

export function publicTools(tools: Tool[]): Tool[] {
  const names = new Set<string>();
  const result: Tool[] = [];
  for (const tool of tools) {
    if (names.has(tool.name)) throw new LocalError('Reah returned duplicate tool definitions.');
    names.add(tool.name);
    const protocol = localProtocol(tool);
    // Keep the private signing-context endpoint hidden on older gateways too.
    if (tool.name === 'reah_signing_context' || protocol?.mode === 'internal') continue;
    if (tool._meta?.[LOCAL_PROTOCOL_META] !== undefined && !protocol) continue;
    const hasSignatureFields = SIGNATURE_FIELDS.some(field => field in (tool.inputSchema.properties ?? {}));
    if (protocol?.mode !== 'signed') {
      // Legacy read-only catalogs work without metadata. Unsigned preparation
      // tools must be explicitly declared by the trusted server.
      if (hasSignatureFields || (!protocol && tool.annotations?.readOnlyHint !== true)) continue;
      result.push(tool);
      continue;
    }
    if (tool.annotations?.readOnlyHint !== false) continue;
    const inputSchema = structuredClone(tool.inputSchema);
    for (const field of SIGNATURE_FIELDS) delete inputSchema.properties?.[field];
    if (inputSchema.required) inputSchema.required = inputSchema.required.filter(field => !SIGNATURE_FIELDS.some(value => value === field));
    result.push({ ...tool, inputSchema });
  }
  return result;
}
