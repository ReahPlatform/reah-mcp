import { mkdir, open, lstat } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import lockfile from 'proper-lockfile';
import type { Config } from './config.js';
import { LocalError } from './config.js';
import type { EnrollmentReceipt } from './enrollment.js';
import type { DeviceKey } from './signing.js';

export interface Tokens { accessToken: string; refreshToken: string; expiresAt: number }
export interface Session {
  version: 1; server: string; clientId: string; deviceKey: DeviceKey;
  tokens?: Tokens;
  credentialEnrollmentRequested?: boolean;
  enrollment?: EnrollmentReceipt;
  pendingRevocation?: string;
}
export interface Store {
  read(): Promise<Session | null>;
  write(session: Session): Promise<void>;
  remove(): Promise<void>;
  exclusive<T>(action: () => Promise<T>): Promise<T>;
}

export class KeychainStore implements Store {
  constructor(private readonly config: Config,
    private readonly coordinationDirectory = join(homedir(), '.reah-mcp')) {}

  private async entry() {
    const { AsyncEntry } = await import('@napi-rs/keyring');
    return new AsyncEntry('Reah MCP Local', this.config.id, { linux: { store: 'secret-service' } });
  }

  async read(): Promise<Session | null> {
    try {
      const raw = await (await this.entry()).getPassword();
      if (raw === null) return null;
      const state = JSON.parse(raw) as Session;
      if (state.version !== 1 || state.server !== this.config.server || typeof state.clientId !== 'string' ||
          !state.deviceKey?.privateKey || !state.deviceKey.publicKey ||
          (state.credentialEnrollmentRequested !== undefined && typeof state.credentialEnrollmentRequested !== 'boolean') ||
          (state.pendingRevocation !== undefined && typeof state.pendingRevocation !== 'string') ||
          (state.tokens && (typeof state.tokens.accessToken !== 'string' ||
            typeof state.tokens.refreshToken !== 'string' || !Number.isFinite(state.tokens.expiresAt)))) {
        throw new Error('Invalid credentials');
      }
      return state;
    } catch {
      throw new LocalError('Cannot read the Reah OS credential store. Unlock it or reconnect; no file fallback is used.');
    }
  }

  async write(session: Session): Promise<void> {
    const value = JSON.stringify(session);
    if (process.platform === 'win32' && Buffer.byteLength(value, 'utf16le') > 2560) {
      throw new LocalError('Reah credentials exceed this OS credential store limit. Use a shorter server URL or reconnect; no file fallback is used.');
    }
    try { await (await this.entry()).setPassword(value); }
    catch { throw new LocalError('Cannot save Reah credentials in the OS credential store.'); }
  }

  async remove(): Promise<void> {
    try { await (await this.entry()).deleteCredential(); }
    catch { throw new LocalError('Cannot remove Reah credentials from the OS credential store.'); }
  }

  async exclusive<T>(action: () => Promise<T>): Promise<T> {
    // Only empty coordination files live on disk. Tokens and keys stay in the OS vault.
    const directory = this.coordinationDirectory;
    await mkdir(directory, { recursive: true, mode: 0o700 });
    const stat = await lstat(directory);
    if (!stat.isDirectory() || (process.platform !== 'win32' && ((stat.mode & 0o077) !== 0 || stat.uid !== process.getuid?.()))) {
      throw new LocalError('The ~/.reah-mcp coordination directory must be private and owned by this user.');
    }
    const path = join(directory, this.config.id);
    const handle = await open(path, 'a', 0o600);
    await handle.close();
    let release: (() => Promise<void>);
    try {
      release = await lockfile.lock(path, {
        stale: 30_000, update: 5_000, retries: { retries: 60, minTimeout: 500, maxTimeout: 1_000 },
        onCompromised: () => {
          // An awaited action must never resume and overwrite a newer login.
          process.stderr.write('Reah credential lock was lost. Reconnect before continuing.\n');
          process.exit(1);
        },
      });
    } catch { throw new LocalError('Another Reah login or refresh is still running. Try again shortly.'); }
    try {
      return await action();
    } finally { await release().catch(() => undefined); }
  }
}
