#!/usr/bin/env node
import { parseArgs } from 'node:util';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { configure, LocalError, safeError } from './config.js';
import { KeychainStore } from './store.js';
import { ReahOAuth } from './oauth.js';
import { RemoteReah, serve } from './proxy.js';

async function main() {
  const { values, positionals } = parseArgs({ allowPositionals: true, options: {
    'enroll-key': { type: 'boolean' }, server: { type: 'string' }, profile: { type: 'string' }, help: { type: 'boolean', short: 'h' },
  } });
  if (values.help) {
    console.log(`Usage: reah-mcp [serve|login|enroll-key|logout|status] [--server HTTPS_URL] [--profile NAME]

login    Open Reah browser consent and store credentials in the OS credential store.
serve    Serve reviewed read-only tools over MCP stdio (default). Run login first.
enroll-key Retry possession proof for the current bound connection.
status   Show local credential and preparation status.
login --enroll-key  Opt into key binding and possession proof (requires server support).
logout   Revoke this connection and remove local credentials and the device key.

Default server: https://platform.stg.reah.com/mcp
Device signing keys are not enrolled for financial operations in this release.`);
    return;
  }
  if (positionals.length > 1 || !['serve', 'login', 'enroll-key', 'logout', 'status'].includes(positionals[0] ?? 'serve')) {
    throw new LocalError('Invalid command. Run reah-mcp --help for usage.');
  }
  if (values['enroll-key'] && positionals[0] !== 'login') throw new LocalError('--enroll-key is only valid with login.');
  const config = configure(values.server, values.profile);
  const oauth = new ReahOAuth(config, new KeychainStore(config));
  switch (positionals[0] ?? 'serve') {
    case 'login': {
      const { default: open } = await import('open');
      process.stderr.write('Opening Reah sign-in. Select the businesses to authorize in your browser.\n');
      await oauth.login(async url => { await open(url, { wait: false }); }, 300_000, values['enroll-key'] ?? false);
      process.stderr.write('Reah connected. Local tools use your newly authorized connection.\n');
      break;
    }
    case 'enroll-key': await oauth.enrollKey(); process.stderr.write('Key possession confirmed; Turnkey registration is still pending. No signing authority is enabled.\n'); break;
    case 'logout': await oauth.logout(); process.stderr.write('Reah connection revoked; local credentials removed.\n'); break;
    case 'status': console.log(JSON.stringify(await oauth.status(), null, 2)); break;
    default: {
      const running = await serve(new RemoteReah(oauth), new StdioServerTransport());
      const close = () => { void running.close().then(() => process.exit(0), () => process.exit(1)); };
      process.once('SIGINT', close); process.once('SIGTERM', close); process.stdin.once('end', close);
    }
  }
}

void main().catch(error => { process.stderr.write(`${safeError(error)}\n`); process.exitCode = 1; });
