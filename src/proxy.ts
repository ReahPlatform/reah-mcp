import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { CallToolRequestSchema, CallToolResultSchema, ListToolsRequestSchema, type Tool } from '@modelcontextprotocol/sdk/types.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import type { ReahOAuth } from './oauth.js';
import { LocalError, safeError, VERSION } from './config.js';
import { callSignedTool } from './transfers.js';
import { localProtocol, publicTools, SIGNATURE_FIELDS } from './catalog.js';

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
    this.tools = publicTools(tools);
    return this.tools;
  }

  async call(name: string, args: Record<string, unknown>, signal?: AbortSignal) {
    // Only the authenticated server's published catalog determines availability.
    if (!this.tools.length) await this.list(signal);
    const tool = this.tools.find(tool => tool.name === name);
    if (!tool) throw new LocalError('This tool is not available in the current Reah catalog.');
    if (SIGNATURE_FIELDS.some(field => Object.hasOwn(args, field))) throw new LocalError('Signing material is supplied by the local client, not tool arguments.');
    const protocol = localProtocol(tool);
    if (protocol?.mode === 'signed') return callSignedTool(name,protocol.signing,args,await this.oauth.signingKey(),this.oauth.config.server,
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
