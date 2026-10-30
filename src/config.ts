import { createHash } from 'node:crypto';

export const DEFAULT_SERVER = 'https://platform.stg.reah.com/mcp';
export const SCOPE = 'reah.read';
export const VERSION = '0.1.0';

export interface Config { server: string; origin: string; profile: string; id: string }

export function configure(server = DEFAULT_SERVER, profile = 'default'): Config {
  let url: URL;
  try { url = new URL(server); }
  catch { throw new LocalError('Use a complete HTTPS MCP URL.'); }
  if (url.protocol !== 'https:' || url.pathname !== '/mcp' || url.search || url.hash ||
      url.username || url.password || server !== url.href || !/^[a-zA-Z0-9_-]{1,64}$/.test(profile)) {
    throw new LocalError('Use an HTTPS /mcp URL without credentials or query parameters and a simple profile name.');
  }
  return { server, origin: url.origin, profile,
    id: createHash('sha256').update(`${server}\n${profile}`).digest('hex') };
}

export class LocalError extends Error {}

export function safeError(error: unknown): string {
  return error instanceof LocalError ? error.message : 'Reah could not complete the request. Check connectivity and try again.';
}
