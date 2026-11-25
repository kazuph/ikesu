// 許可リスト devices.json・Noise の鍵 noise-key.json・中継の設定 relay.json の読み書き。
// 壊れたファイルは失敗として扱い、推測で直さない。書くときは 0600 で一時ファイルから置き換える。
import { createHash, createPublicKey, randomBytes } from 'node:crypto';
import os from 'node:os';
import { constants, lstatSync, unlinkSync } from 'node:fs';
import fs from 'node:fs/promises';
import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import dh from 'noise-handshake/dh.js';
import { appTag, MAX_ALLOW } from './relay-client.mjs';

export const b64u = bytes => Buffer.from(bytes).toString('base64url');
// 鍵の指紋: SHA-256 の先頭8バイト（アプリの RelayMath.fingerprint と同じ形）。
export const fingerprint = bytes => [...createHash('sha256').update(bytes).digest().subarray(0, 8)].map(byte => byte.toString(16).padStart(2, '0')).join(':');
export const fromB64u = (text, length) => {
  if (typeof text !== 'string' || !/^[A-Za-z0-9_-]*$/.test(text)) throw Error('base64url ではありません');
  const bytes = Buffer.from(text, 'base64url');
  if (length !== undefined && bytes.length !== length) throw Error(`長さが ${length} バイトではありません`);
  if (b64u(bytes) !== text) throw Error('base64url が正規の形ではありません');
  return bytes;
};
export function validOperatePublicKey(text) {
  try {
    const key = createPublicKey({ key: fromB64u(text), format: 'der', type: 'spki' });
    return key.asymmetricKeyType === 'ec' && key.asymmetricKeyDetails?.namedCurve === 'prime256v1';
  } catch { return false; }
}
export const PERMISSIONS = ['notify', 'answer', 'screen', 'input'];

async function readJson(file, { optional = false } = {}) {
  let handle;
  try { handle = await fs.open(file, constants.O_RDONLY | constants.O_NOFOLLOW); }
  catch (error) { if (optional && error.code === 'ENOENT') return null; throw error; }
  try {
    const stat = await handle.stat();
    if (!stat.isFile() || (stat.mode & 0o077) !== 0) throw Error(`${path.basename(file)} の権限が不正です（0600 にしてください）`);
    try { return JSON.parse(await handle.readFile('utf8')); }
    catch { throw Error(`${path.basename(file)} が壊れています。推測では直しません`); }
  } finally { await handle.close(); }
}

export async function writeJson(file, value) {
  await fs.mkdir(path.dirname(file), { recursive: true, mode: 0o700 });
  const temporary = path.join(path.dirname(file), `.${path.basename(file)}-${randomBytes(8).toString('hex')}`);
  const handle = await fs.open(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600);
  try { await handle.writeFile(JSON.stringify(value, null, 2)); await handle.sync(); }
  finally { await handle.close(); }
  try { await fs.rename(temporary, file); } catch (error) { await fs.unlink(temporary).catch(() => {}); throw error; }
}

export async function loadNoiseKey(configDir) {
  const file = path.join(configDir, 'noise-key.json');
  let value = await readJson(file, { optional: true });
  if (!value) {
    const pair = dh.generateKeyPair();
    value = { public: b64u(pair.publicKey), secret: b64u(pair.secretKey) };
    await writeJson(file, value);
  }
  const publicKey = fromB64u(value?.public, 32);
  const secretKey = fromB64u(value?.secret, 32);
  return { publicKey, secretKey };
}

export async function loadRelay(configDir) {
  const value = await readJson(path.join(configDir, 'relay.json'), { optional: true });
  if (!value) return null;
  if (typeof value.url !== 'string' || !/^wss?:\/\//.test(value.url)) throw Error('relay.json の url が不正です');
  fromB64u(value.room, 32); fromB64u(value.relay_key, 32);
  return { url: value.url.replace(/\/+$/, ''), room: value.room, relayKey: fromB64u(value.relay_key, 32) };
}

export async function saveRelay(configDir, { url, room = b64u(randomBytes(32)), relayKey = randomBytes(32) }) {
  if (!/^wss:\/\/[^/\s]+$/.test(url.replace(/\/+$/, '')) && !/^ws:\/\/(127\.0\.0\.1|localhost)(:\d+)?$/.test(url.replace(/\/+$/, ''))) throw Error('url は wss://… の形で指定してください');
  fromB64u(room, 32);
  await writeJson(path.join(configDir, 'relay.json'), { url: url.replace(/\/+$/, ''), room, relay_key: b64u(relayKey) });
  return { room, relayKey: b64u(relayKey) };
}

const DEVICE_KEYS = ['device', 'noise_pub', 'operate_pub', 'name', 'permissions', 'created'];
function validDevice(device) {
  if (!device || typeof device !== 'object') return false;
  try {
    fromB64u(device.device, 16); fromB64u(device.noise_pub, 32); fromB64u(device.operate_pub);
    return typeof device.name === 'string' && Array.isArray(device.permissions) && device.permissions.every(item => PERMISSIONS.includes(item)) && typeof device.created === 'string'
      && DEVICE_KEYS.every(key => key in device);
  } catch { return false; }
}

export async function loadDevices(configDir) {
  const value = await readJson(path.join(configDir, 'devices.json'), { optional: true });
  if (!value) return [];
  if (!Array.isArray(value.devices) || !value.devices.every(validDevice)) throw Error('devices.json の形が不正です。推測では直しません');
  return value.devices;
}

export async function saveDevices(configDir, devices) { await writeJson(path.join(configDir, 'devices.json'), { devices }); }

const DEVICE_LOCK_FLAGS = constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW;
const DEVICE_LOCK_STALE_MS = 30_000;
const DEVICE_LOCK_WAIT_MS = 10_000;
const DEVICE_LOCK_RETRY_MS = 20;

function unlinkDevicesLock(file, owner) {
  try {
    // Keep the identity check and unlink together without yielding to the event loop.
    const current = lstatSync(file);
    if (current.dev === owner.dev && current.ino === owner.ino) unlinkSync(file);
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
}

function lockOwnerExited(text) {
  const pid = Number(text.trim());
  // A new owner may have created the file but not yet written its PID.
  if (!Number.isInteger(pid) || pid <= 0 || pid > 0x7fffffff) return false;
  try {
    process.kill(pid, 0);
    return false;
  } catch (error) {
    if (error.code === 'ESRCH') return true;
    if (error.code === 'EPERM') return false;
    throw error;
  }
}

async function reclaimDevicesLock(file) {
  let lock;
  try { lock = await fs.open(file, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK); }
  catch (error) {
    if (error.code === 'ENOENT') return true;
    throw error;
  }
  try {
    const owner = await lock.stat();
    if (!owner.isFile()) return false;
    if (Date.now() - owner.mtimeMs <= DEVICE_LOCK_STALE_MS && !lockOwnerExited(await lock.readFile('utf8'))) return false;
    unlinkDevicesLock(file, owner);
    return true;
  } finally {
    await lock.close();
  }
}

export async function withDevicesLock(configDir, fn) {
  await fs.mkdir(configDir, { recursive: true, mode: 0o700 });
  const file = path.join(configDir, 'devices.lock');
  const deadline = performance.now() + DEVICE_LOCK_WAIT_MS;
  while (performance.now() < deadline) {
    let lock;
    try { lock = await fs.open(file, DEVICE_LOCK_FLAGS, 0o600); }
    catch (error) {
      if (error.code !== 'EEXIST') throw error;
      if (!await reclaimDevicesLock(file)) {
        await delay(Math.min(DEVICE_LOCK_RETRY_MS, Math.max(0, deadline - performance.now())));
      }
      continue;
    }
    try {
      const owner = await lock.stat();
      try {
        await lock.writeFile(String(process.pid));
        return await fn();
      } finally {
        unlinkDevicesLock(file, owner);
      }
    } finally {
      await lock.close();
    }
  }
  throw Error('devices.json の更新が混み合っています。もう一度試してください');
}

export function addDevice(configDir, device) {
  return withDevicesLock(configDir, async () => {
    const devices = await loadDevices(configDir);
    if (devices.length >= MAX_ALLOW) throw Error(`登録できる端末は ${MAX_ALLOW} 台までです`);
    devices.push(device);
    await saveDevices(configDir, devices);
    return device;
  });
}

export function revokeDevice(configDir, id) {
  return withDevicesLock(configDir, async () => {
    const devices = await loadDevices(configDir);
    const kept = devices.filter(device => device.device !== id);
    if (kept.length === devices.length) return false;
    await saveDevices(configDir, kept);
    return true;
  });
}

// 公開鍵を手で登録する（アプリを QR で読めないとき）。接続情報（1行の JSON）を返す。秘密は含めない。
export async function addDeviceByHand(configDir, { name, noise, operate, pcName, permissions = PERMISSIONS }) {
  if (typeof name !== 'string' || !name.trim() || name.length > 64) throw Error('端末名が不正です');
  if (!permissions.length || !permissions.every(item => PERMISSIONS.includes(item))) throw Error(`権限は ${PERMISSIONS.join(',')} から選んでください`);
  const noiseBytes = fromB64u(noise, 32);
  if (!validOperatePublicKey(operate)) throw Error('操作用の公開鍵が P-256 の SPKI DER ではありません');
  const relay = await loadRelay(configDir);
  if (!relay) throw Error('relay.json がありません。先に relay-setup を実行してください');
  const device = { device: b64u(randomBytes(16)), noise_pub: noise, operate_pub: operate, name: name.trim(), permissions: [...new Set(permissions)], created: new Date().toISOString() };
  await addDevice(configDir, device);
  const { publicKey } = await loadNoiseKey(configDir);
  const link = JSON.stringify({ v: 1, kind: 'ikesu-link', relay: relay.url, room: relay.room, bridge: b64u(publicKey), device: device.device,
    tag: appTag(relay, Buffer.from(device.device, 'base64url')), name: pcName || os.hostname() });
  return { link, device, fingerprints: { device: fingerprint(noiseBytes), bridge: fingerprint(publicKey) } };
}
