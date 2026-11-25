import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { on, once } from 'node:events';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
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
    process.send('starting');
    try {
      const result = operation === 'add' ? await addDevice(configDir, value) : await revokeDevice(configDir, value);
      process.send({ result });
    } catch (error) {
      process.send({ error: error.message });
    }
  });
  process.send('ready');
`;

function spawnWorker(t, script, ...args) {
  const moduleUrl = new URL('../devices.mjs', import.meta.url).href;
  const child = spawn(process.execPath, ['--input-type=module', '-e', script, moduleUrl, ...args], {
    stdio: ['ignore', 'ignore', 'inherit', 'ipc'],
  });
  const exited = once(child, 'exit');
  t.after(async () => {
    if (child.exitCode === null && child.signalCode === null) child.kill();
    await exited;
  });
  return { child, exited };
}

async function startWorker(t, operation) {
  const { child } = spawnWorker(t, workerScript, operation);
  const [ready] = await once(child, 'message', { signal: t.signal });
  assert.equal(ready, 'ready');
  return async (configDir, value, onStarted) => {
    const messages = on(child, 'message', { signal: t.signal, close: ['exit'] });
    try {
      child.send({ configDir, value });
      const { value: [status] = [] } = await messages.next();
      assert.equal(status, 'starting');
      onStarted?.();
      const { value: [reply] = [] } = await messages.next();
      assert.ok(reply, 'worker exited before returning a result');
      assert.equal(reply.error, undefined);
      return reply.result;
    } finally {
      await messages.return();
    }
  };
}

const holderScript = `
  import { once } from 'node:events';
  import fs from 'node:fs/promises';
  import path from 'node:path';
  const { saveDevices, withDevicesLock } = await import(process.argv[1]);
  const configDir = process.argv[2];
  const devices = JSON.parse(process.argv[3]);
  await withDevicesLock(configDir, async () => {
    await saveDevices(configDir, devices);
    if (process.argv[4] === 'backdate') {
      const old = new Date(Date.now() - 60_000);
      await fs.utimes(path.join(configDir, 'devices.lock'), old, old);
    }
    const command = once(process, 'message');
    process.send('holding');
    if ((await command)[0] === 'exit') process.exit(0);
  });
  process.disconnect();
`;

async function holdLock(t, configDir, devices, { backdate = false } = {}) {
  const { child, exited } = spawnWorker(t, holderScript, configDir, JSON.stringify(devices), backdate ? 'backdate' : '');
  const [status] = await once(child, 'message', { signal: t.signal });
  assert.equal(status, 'holding');
  return async command => {
    child.send(command);
    assert.deepEqual(await exited, [0, null]);
  };
}

function startRegistration(add, configDir, device) {
  const started = Promise.withResolvers();
  let finished = false;
  const result = add(configDir, device, started.resolve).finally(() => { finished = true; });
  result.catch(started.reject);
  return { started: started.promise, result, get finished() { return finished; } };
}

async function assertRegistrationsWaiting(registrations) {
  await Promise.all(registrations.map(registration => registration.started));
  // Observe several 20 ms retry intervals while the holder stays at the IPC barrier.
  await delay(100);
  assert.ok(registrations.every(registration => !registration.finished));
}

test('B10 別プロセスから登録と revoke を同時に20回行い、毎回 C が残り B が消える', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const [add, revoke] = await Promise.all([startWorker(t, 'add'), startWorker(t, 'revoke')]);
  const a = device('A'), b = device('B'), c = device('C');
  let lockInode;
  for (let i = 0; i < 20; i++) {
    await saveDevices(configDir, [a, b]);
    const [added, revoked] = await Promise.all([add(configDir, c), revoke(configDir, b.device)]);
    assert.deepEqual(added, c);
    assert.equal(revoked, true);
    assert.deepEqual(await loadDevices(configDir), [a, c], `iteration ${i + 1}`);
    const { ino } = await fs.stat(lockPath);
    lockInode ??= ino;
    assert.equal(ino, lockInode);
  }
});

test('B11 ロックの mtime が1分前でも保持中は更新せず、解放後に最新の一覧へ登録する', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const a = device('A'), b = device('B'), c = device('C');
  await saveDevices(configDir, [a]);
  const [release, add] = await Promise.all([holdLock(t, configDir, [b], { backdate: true }), startWorker(t, 'add')]);
  const { mtimeMs, ino } = await fs.stat(lockPath);
  assert.ok(Date.now() - mtimeMs >= 60_000);
  const before = await fs.readFile(path.join(configDir, 'devices.json'));
  assert.deepEqual(await loadDevices(configDir), [b]);

  const registration = startRegistration(add, configDir, c);
  await assertRegistrationsWaiting([registration]);
  assert.deepEqual(await fs.readFile(path.join(configDir, 'devices.json')), before);
  await release('release');
  assert.deepEqual(await registration.result, c);
  assert.deepEqual(await loadDevices(configDir), [b, c]);
  assert.equal((await fs.stat(lockPath)).ino, ino);
});

test('B12 更新が例外で終わってもロックは解放され、同じファイルで次の登録が成功する', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const failure = new Error('update failed');
  let lockInode;
  await assert.rejects(withDevicesLock(configDir, async () => {
    const { ino, mode } = await fs.stat(lockPath);
    assert.equal(mode & 0o777, 0o600);
    lockInode = ino;
    throw failure;
  }), error => error === failure);
  const a = device('A');
  assert.deepEqual(await addDevice(configDir, a), a);
  assert.equal((await fs.stat(lockPath)).ino, lockInode);
});

test('B13 所有プロセスが close せず終了しても、次の登録でロックを取れる', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const a = device('A'), b = device('B');
  const exit = await holdLock(t, configDir, [a]);
  const { ino } = await fs.stat(lockPath);
  await exit('exit');
  assert.deepEqual(await addDevice(configDir, b), b);
  assert.deepEqual(await loadDevices(configDir), [a, b]);
  assert.equal((await fs.stat(lockPath)).ino, ino);
});

test('B14 所有プロセスの終了を待つ3プロセスが、異なる端末をすべて登録できる', { timeout: 5000 }, async t => {
  const { configDir, lockPath } = await configFixture(t);
  const [exit, ...workers] = await Promise.all([
    holdLock(t, configDir, []),
    startWorker(t, 'add'), startWorker(t, 'add'), startWorker(t, 'add'),
  ]);
  const { ino } = await fs.stat(lockPath);
  const entries = ['A', 'B', 'C'].map(device);
  const registrations = workers.map((add, index) => startRegistration(add, configDir, entries[index]));
  await assertRegistrationsWaiting(registrations);
  assert.deepEqual(await loadDevices(configDir), []);
  await exit('exit');
  assert.deepEqual(await Promise.all(registrations.map(registration => registration.result)), entries);
  const saved = await loadDevices(configDir);
  assert.equal(saved.length, entries.length);
  assert.deepEqual(new Set(saved), new Set(entries));
  assert.equal((await fs.stat(lockPath)).ino, ino);
});
