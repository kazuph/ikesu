import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { addDevice, b64u, loadDevices, saveDevices, withDevicesLock } from '../devices.mjs';

function device(name) {
  return {
    device: b64u(Buffer.alloc(16, name.charCodeAt(0))),
    noise_pub: b64u(Buffer.alloc(32)),
    operate_pub: b64u(Buffer.of(1)),
    name,
    permissions: ['notify'],
    created: '2026-11-25T00:00:00.000Z',
  };
}

async function configFixture(t) {
  const configDir = await fs.mkdtemp(path.join(os.tmpdir(), 'ikesu-devices-lock-'));
  t.after(() => fs.rm(configDir, { recursive: true, force: true }));
  return { configDir, lockPath: path.join(configDir, 'devices.lock') };
}

const workerScript = `
  const { addDevice, revokeDevice } = await import(process.argv[1]);
  const operation = process.argv[2];
  process.on('message', async ({ configDir, value }) => {
    try {
      const result = operation === 'add' ? await addDevice(configDir, value) : await revokeDevice(configDir, value);
      process.send({ result });
    } catch (error) {
      process.send({ error: error.message });
    }
  });
  process.send('ready');
`;

async function startWorker(t, operation) {
  const moduleUrl = new URL('../devices.mjs', import.meta.url).href;
  const child = spawn(process.execPath, ['--input-type=module', '-e', workerScript, moduleUrl, operation], {
    stdio: ['ignore', 'ignore', 'inherit', 'ipc'],
  });
  const exited = once(child, 'exit');
  t.after(async () => {
    if (child.connected) child.disconnect();
    await exited;
  });
  const [ready] = await once(child, 'message', { signal: t.signal });
  assert.equal(ready, 'ready');
  return async (configDir, value) => {
    const result = once(child, 'message', { signal: t.signal });
    child.send({ configDir, value });
    const [reply] = await result;
    assert.equal(reply.error, undefined);
    return reply.result;
  };
}

test('B10 別プロセスから登録と revoke を同時に20回行い、毎回 C が残り B が消える', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const [add, revoke] = await Promise.all([startWorker(t, 'add'), startWorker(t, 'revoke')]);
  const a = device('A'), b = device('B'), c = device('C');
  for (let i = 0; i < 20; i++) {
    await saveDevices(configDir, [a, b]);
    const [added, revoked] = await Promise.all([add(configDir, c), revoke(configDir, b.device)]);
    assert.deepEqual(added, c);
    assert.equal(revoked, true);
    assert.deepEqual(await loadDevices(configDir), [a, c], `iteration ${i + 1}`);
    await assert.rejects(fs.stat(lockPath), { code: 'ENOENT' });
  }
});

test('B11 死んだ PID や30秒より古い devices.lock を回収して登録できる', async t => {
  const { configDir, lockPath } = await configFixture(t);
  const child = spawn(process.execPath, ['-e', ''], { stdio: 'ignore' });
  assert.deepEqual(await once(child, 'exit'), [0, null]);
  assert.throws(() => process.kill(child.pid, 0), { code: 'ESRCH' });
  const old = new Date(Date.now() - 31_000);
  const cases = [
    { name: 'A', pid: child.pid, mtime: old },
    { name: 'B', pid: child.pid, mtime: new Date() },
    { name: 'C', pid: process.pid, mtime: old },
  ];
  for (const { name, pid, mtime } of cases) {
    await fs.writeFile(lockPath, String(pid), { flag: 'wx', mode: 0o600 });
    await fs.utimes(lockPath, mtime, mtime);
    const entry = device(name);
    assert.deepEqual(await addDevice(configDir, entry), entry);
    await assert.rejects(fs.stat(lockPath), { code: 'ENOENT' });
  }
  assert.deepEqual(await loadDevices(configDir), cases.map(({ name }) => device(name)));
});

test('B12 更新が例外で終わっても devices.lock が残らず、次の登録が成功する', async t => {
  const { configDir, lockPath } = await configFixture(t);
  const failure = new Error('update failed');
  await assert.rejects(withDevicesLock(configDir, async () => {
    assert.equal(await fs.readFile(lockPath, 'utf8'), String(process.pid));
    assert.equal((await fs.stat(lockPath)).mode & 0o777, 0o600);
    throw failure;
  }), error => error === failure);
  await assert.rejects(fs.stat(lockPath), { code: 'ENOENT' });
  const a = device('A');
  assert.deepEqual(await addDevice(configDir, a), a);
});
