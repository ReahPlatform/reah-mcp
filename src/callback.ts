import { createServer, type ServerResponse } from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { LocalError } from './config.js';

export async function callbackListener(state: string, issuer: string, path: string, timeoutMs = 300_000) {
  let accept!: (value: { code: string; finish: (ok: boolean) => void }) => void;
  let reject!: (reason: Error) => void;
  let pending: ServerResponse | undefined;
  let used = false;
  const result = new Promise<{ code: string; finish: (ok: boolean) => void }>((resolve, fail) => {
    accept = resolve; reject = fail;
  });
  // A timeout can fire while the browser launcher is still resolving.
  void result.catch(() => undefined);
  const server = createServer((req, res) => {
    res.setHeader('Cache-Control', 'no-store');
    res.setHeader('Content-Type', 'text/plain; charset=utf-8');
    res.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'");
    res.setHeader('Referrer-Policy', 'no-referrer');
    let url: URL;
    try { url = new URL(req.url ?? '/', 'http://127.0.0.1'); }
    catch { res.writeHead(400); res.end('Invalid OAuth callback.'); return; }
    const receivedState = Buffer.from(url.searchParams.get('state') ?? '');
    const expectedState = Buffer.from(state);
    const valid = req.method === 'GET' && req.headers.host === new URL(redirectUri).host &&
      !req.headers.origin && url.pathname === path && !used &&
      [...url.searchParams.keys()].every(key => url.searchParams.getAll(key).length === 1) &&
      receivedState.length === expectedState.length && timingSafeEqual(receivedState, expectedState) &&
      (!url.searchParams.has('iss') || url.searchParams.get('iss') === issuer);
    if (!valid) { res.writeHead(400); res.end('Invalid OAuth callback.'); return; }
    const code = url.searchParams.get('code');
    if (url.searchParams.has('error') || !code || code.length > 4096) {
      used = true; res.writeHead(400); res.end('Authorization was not completed.');
      reject(new LocalError('Authorization was denied or invalid. Start a new login.')); return;
    }
    used = true; pending = res; clearTimeout(timer);
    accept({ code, finish: ok => {
      if (!res.writableEnded) {
        res.writeHead(ok ? 200 : 400);
        res.end(ok ? 'Reah authentication complete. You may close this window.' : 'Reah authentication failed. Start a new login.');
      }
    } });
  });
  server.requestTimeout = 15_000;
  let redirectUri = '';
  await new Promise<void>((resolve, fail) => {
    server.once('error', fail);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Cannot bind OAuth callback.');
  redirectUri = `http://127.0.0.1:${address.port}${path}`;
  const timer = setTimeout(() => {
    pending?.end('Reah authentication timed out. Start a new login.');
    reject(new LocalError('Reah login timed out. Start a new login.'));
    server.closeAllConnections(); server.close();
  }, timeoutMs);
  return {
    redirectUri, result,
    close() { clearTimeout(timer); pending?.end(); server.closeAllConnections(); server.close(); },
  };
}
