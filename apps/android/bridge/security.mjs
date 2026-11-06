import { randomBytes, timingSafeEqual } from 'node:crypto';
import { constants } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import net from 'node:net';

const execute = promisify(execFile);
export const TOKEN_BYTES = 32;

export async function loadToken(configDir) {
  await fs.mkdir(configDir, { recursive: true, mode: 0o700 });
  const file = path.join(configDir, 'token');
  const temporary = path.join(configDir, `.token-${randomBytes(16).toString('hex')}`);
  const created = await fs.open(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try {
    await created.writeFile(randomBytes(TOKEN_BYTES).toString('hex'));
    await created.close();
    try { await fs.link(temporary, file); } catch (error) { if (error.code !== 'EEXIST') throw error; }
  } finally { await created.close().catch(() => {}); await fs.unlink(temporary); }
  const handle = await fs.open(file, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = await handle.stat();
    if (!stat.isFile() || (stat.mode & 0o077) !== 0) throw Error('合言葉ファイルの権限が不正です');
    const token = (await handle.readFile('utf8')).trim();
    if (!/^[0-9a-f]{64}$/.test(token)) throw Error('合言葉ファイルが不正です');
    return token;
  } finally { await handle.close(); }
}

export function tokenMatches(expected, supplied) {
  if (typeof expected !== 'string' || !/^[0-9a-f]{64}$/.test(expected) || typeof supplied !== 'string' || !/^[0-9a-f]{64}$/.test(supplied)) return false;
  return timingSafeEqual(Buffer.from(expected, 'hex'), Buffer.from(supplied, 'hex'));
}

export async function rotateToken(configDir) {
  await fs.mkdir(configDir, { recursive: true, mode: 0o700 });
  const file = path.join(configDir, 'token');
  const temporary = path.join(configDir, `.token-${randomBytes(16).toString('hex')}`);
  const handle = await fs.open(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await handle.writeFile(randomBytes(TOKEN_BYTES).toString('hex')); await handle.sync(); }
  finally { await handle.close(); }
  try { await fs.rename(temporary, file); } catch (error) { await fs.unlink(temporary); throw error; }
  return loadToken(configDir);
}

export async function tailscaleAddress(configuredPath) {
  const candidates = [configuredPath || 'tailscale', '/Applications/Tailscale.app/Contents/MacOS/Tailscale'];
  for (const candidate of candidates) {
    try {
      const { stdout } = await execute(candidate, ['ip', '-4'], { timeout: 10000 });
      const addresses = stdout.trim().split(/\s+/);
      if (addresses.length === 1 && net.isIPv4(addresses[0])) return addresses[0];
    } catch { /* Try the next configured location. */ }
  }
  throw Error('Tailscale の IPv4 アドレスを取得できません');
}
