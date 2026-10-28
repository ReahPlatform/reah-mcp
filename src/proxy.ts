import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { CallToolRequestSchema, CallToolResultSchema, ListToolsRequestSchema, type Tool } from '@modelcontextprotocol/sdk/types.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import type { ReahOAuth } from './oauth.js';
import { LocalError, safeError, VERSION } from './config.js';
import { callFinancialTool, WRITE_TOOLS } from './transfers.js';

// New upstream tools require review before a local client release exposes them.
const READS = new Set([
  'reah_list_entities', 'reah_catalog', 'reah_get_entity', 'reah_list_balances', 'reah_transfer_sources',
  'reah_list_wallets', 'reah_list_banking_accounts', 'reah_get_transaction', 'reah_list_transactions',
  'reah_list_settled_movements', 'reah_list_contacts', 'reah_list_invoices', 'reah_get_invoice',
  'reah_list_approvals', 'reah_list_cards', 'reah_list_card_accounts', 'reah_list_card_transactions',
  'reah_list_treasury_positions', 'reah_list_members', 'reah_list_assets', 'reah_search',
]);

// Preparation may lazily initialize an empty Gas Station, but never funds it.
const PREPARATIONS = new Set(['reah_estimate_transfer']);

export function reviewedTools(tools: Tool[]): Tool[] {
  const names = new Set<string>();
  return tools.filter(tool => {
    if (names.has(tool.name)) throw new LocalError('Reah returned duplicate tool definitions.');
    names.add(tool.name);
    return (READS.has(tool.name) && tool.annotations?.readOnlyHint === true) || ((WRITE_TOOLS.has(tool.name) || PREPARATIONS.has(tool.name)) && tool.annotations?.readOnlyHint === false);
  }).map(tool => {
    if (!WRITE_TOOLS.has(tool.name)) return tool;
    const inputSchema = structuredClone(tool.inputSchema);
    for (const key of ['_proof','requestSignature','signatures']) delete inputSchema.properties?.[key];
    if (inputSchema.required) inputSchema.required = inputSchema.required.filter(key => !['_proof','requestSignature','signatures'].includes(key));
    return { ...tool, inputSchema };
  });
}

export class RemoteReah {
  private client: Client | undefined;
  private connecting: Promise<Client> | undefined;
  private tools: Tool[] = [];

  constructor(private readonly oauth: ReahOAuth, private readonly fetch: typeof globalThis.fetch = globalThis.fetch) {}

  private async connected(): Promise<Client> {
    if (this.client) return this.client;
    if (!this.connecting) this.connecting = (async () => {
      const client = new Client({ name: 'reah-local-mcp', version: VERSION });
      client.onclose = () => { if (this.client === client) { this.client = undefined; this.tools = []; } };
      const authenticatedFetch: typeof globalThis.fetch = async (input, init) => {
        const target = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        if (target !== this.oauth.config.server) throw new LocalError('Blocked an unexpected MCP request destination.');
        const headers = new Headers(init?.headers);
        headers.set('Authorization', `Bearer ${await this.oauth.accessToken()}`);
        const response = await this.fetch(input, { ...init, headers, redirect: 'error',
          signal: AbortSignal.any([...(init?.signal ? [init.signal] : []), AbortSignal.timeout(60_000)]) });
        if (response.status === 401) {
          await response.body?.cancel();
          throw new LocalError('Reah authorization expired or was revoked. Run reah-mcp login and reconnect the MCP server.');
        }
        return response;
      };
      try {
        await client.connect(new StreamableHTTPClientTransport(new URL(this.oauth.config.server), {
          fetch: authenticatedFetch, redirectPolicy: 'same-origin', reconnectionOptions: {
            maxRetries: 0, initialReconnectionDelay: 1000, maxReconnectionDelay: 1000, reconnectionDelayGrowFactor: 1,
          },
        }), { timeout: 60_000 });
        this.client = client;
        return client;
      } catch (error) { await client.close().catch(() => undefined); throw error; }
      finally { this.connecting = undefined; }
    })();
    return this.connecting;
  }

  async list(signal?: AbortSignal): Promise<Tool[]> {
    const client = await this.connected();
    const tools: Tool[] = [];
    let cursor: string | undefined;
    const seen = new Set<string>();
    do {
      const page = await client.listTools(cursor ? { cursor } : {}, { ...(signal ? { signal } : {}), timeout: 60_000 });
      tools.push(...page.tools);
      cursor = page.nextCursor;
      if (tools.length > 500 || (cursor && seen.has(cursor))) throw new LocalError('Reah returned an invalid tool catalog.');
      if (cursor) seen.add(cursor);
    } while (cursor);
    this.tools = reviewedTools(tools);
    return this.tools;
  }

  async call(name: string, args: Record<string, unknown>, signal?: AbortSignal) {
    // Use the deployed catalog loaded for this connection, without an extra list request per read.
    if (!READS.has(name) && !WRITE_TOOLS.has(name) && !PREPARATIONS.has(name)) throw new LocalError('This tool is not available in the reviewed catalog.');
    if (!this.tools.length) await this.list(signal);
    if (!this.tools.some(tool => tool.name === name)) throw new LocalError('This tool is not available in the reviewed catalog.');
    if (WRITE_TOOLS.has(name)) return callFinancialTool(name,args,await this.oauth.signingKey(),this.oauth.config.server,
      (tool, input, callSignal) => this.invoke(tool,input,callSignal),signal);
    return this.invoke(name,args,signal);
  }

  private async invoke(name: string, args: Record<string, unknown>, signal?: AbortSignal) {
    const client = await this.connected();
    return client.request({ method: 'tools/call', params: { name, arguments: args } },
      CallToolResultSchema, { ...(signal ? { signal } : {}), timeout: 60_000 });
  }

  async close() { await this.client?.close(); this.client = undefined; }
}

export async function serve(remote: RemoteReah, transport: Transport) {
  const server = new Server({ name: 'reah-local-mcp', version: VERSION }, { capabilities: { tools: {} } });
  server.setRequestHandler(ListToolsRequestSchema, async (_, extra) => {
    try { return { tools: await remote.list(extra.signal) }; }
    catch (error) { throw new LocalError(safeError(error)); }
  });
  server.setRequestHandler(CallToolRequestSchema, async (request, extra) => {
    try { return await remote.call(request.params.name, request.params.arguments ?? {}, extra.signal); }
    catch (error) { return { isError: true, content: [{ type: 'text', text: safeError(error) }] }; }
  });
  await server.connect(transport);
  return { async close() { await server.close(); await remote.close(); } };
}
