import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import pty from 'node-pty';
import { b64u, loadDevices, saveRelay } from '../devices.mjs';
import { startMobile } from '../mobile-runtime.mjs';
import { FakeApp, FakeRelay, until } from './mobile-harness.mjs';

const exec = promisify(execFile);
const cwd = fileURLToPath(new URL('../', import.meta.url));
const args = ['./main.mjs', 'pair', '--name', 'pair-test'];

async function fixture(t) {
  const home = await fs.mkdtemp('/tmp/ikesu-pair-');
  const configDir = path.join(home, '.config/ikesu/plugins/config/kazuph.android');
  const stateDir = path.join(home, '.local/state/ikesu/plugins/kazuph.android');
  const lockPath = path.join(stateDir, 'pair.lock');
  const env = {
    ...process.env, HOME: home, PATH: `${path.dirname(process.execPath)}:${process.env.PATH}`,
    IKESU_PLUGIN_CONFIG_DIR: configDir, IKESU_PLUGIN_STATE_DIR: stateDir,
  };
  const relayKey = randomBytes(32), room = b64u(randomBytes(32));
  const relay = await FakeRelay.start(relayKey, room);
  const terminals = [];
  let daemon;
  t.after(async () => {
    for (const term of terminals) term.kill();
    await Promise.all(terminals.map(term => term.result));
    await daemon?.stop();
    await relay.stop();
    await fs.rm(home, { recursive: true, force: true });
  });
  await saveRelay(configDir, { url: relay.url, room, relayKey });
  return {
    configDir, lockPath, relay,
    async startDaemon(decisions = {}) {
      daemon = await startMobile({ configDir, stateDir, log: () => {}, decisions: {
        snapshot: async () => [], answer: async () => ({}), later: async () => {}, ...decisions,
      } });
      await relay.bridgeUp();
      return daemon.hub;
    },
    open(cols = 160, rows = 60) {
      const terminal = pty.spawn(process.execPath, args, { cwd, env, cols, rows });
      let output = '', exited = false;
      terminal.onData(data => { output += data; });
      const result = new Promise(resolve => terminal.onExit(event => { exited = true; resolve(event); }));
      const term = {
        terminal, result,
        get output() { return output; },
        get exited() { return exited; },
        kill(signal) { if (!exited) terminal.kill(signal); },
      };
      terminals.push(term);
      return term;
    },
    manual() { return exec('/bin/sh', ['./pair.command'], { cwd, env }); },
    async readLock() { return JSON.parse(await fs.readFile(lockPath, 'utf8')); },
  };
}

test('一時 bridge は手動起動と排他し、強制終了で残ったロックを回収する', async t => {
  const f = await fixture(t);
  const first = f.open();
  await until(() => first.output.includes('6桁:'));
  assert.doesNotMatch(first.output, /画面が小さい/);
  const owner = await f.readLock();
  assert.equal(owner.pid, first.terminal.pid);
  assert.equal((await fs.stat(f.lockPath)).mode & 0o777, 0o600);
  await assert.rejects(f.manual(), error => error.code === 1 &&
    error.stderr.includes(`既にペアリングが実行中です（PID ${owner.pid}）`));
  assert.deepEqual(await f.readLock(), owner);
  assert.equal(first.exited, false);

  first.kill('SIGKILL');
  await first.result;
  assert.deepEqual(await f.readLock(), owner);
  const contenders = [f.open(), f.open()];
  await until(() => contenders.some(term => term.output.includes('6桁:')) && contenders.some(term => term.exited));
  const entered = contenders.find(term => !term.exited);
  const rejected = contenders.find(term => term.exited);
  assert.equal((await rejected.result).exitCode, 1);
  assert.equal((await f.readLock()).pid, entered.terminal.pid);
  entered.kill('SIGTERM');
  assert.equal((await entered.result).exitCode, 143);
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });
});

test('常駐 bridge の QR を後発が置き換えず、6桁で登録後にロックを解放する', async t => {
  const f = await fixture(t);
  const hub = await f.startDaemon();
  const first = f.open();
  await until(() => first.output.includes('6桁:'));
  const pairing = hub.pairing;
  assert.ok(pairing?.active);
  const second = f.open();
  assert.equal((await second.result).exitCode, 1);
  assert.match(second.output, /既にペアリングが実行中です/);
  assert.equal(hub.pairing, pairing);

  const app = new FakeApp(f.relay, 'pair', pairing.id, hub.noiseKey.publicKey);
  assert.ok(await app.handshake());
  app.sendJson(app.pairMessage(pairing.secret, 'pair-lock test device'));
  await until(() => pairing.candidates.length === 1);
  first.terminal.write(`${app.sas(pairing.secret)}\r`);
  assert.equal((await first.result).exitCode, 0);
  assert.match(first.output, /登録しました/);
  assert.equal((await loadDevices(f.configDir)).length, 1);
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });

  const third = f.open();
  await until(() => third.output.includes('6桁:'));
  third.terminal.write('\x03');
  assert.equal((await third.result).exitCode, 130);
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });
});

test('小さい PTY では待機し、必要寸法で QR の全行と入力欄を描画する', async t => {
  const f = await fixture(t);
  const term = f.open(80, 24);
  const size = await until(() => term.output.match(/現在 80×24 \/ 必要 (\d+)×(\d+)/));
  assert.doesNotMatch(term.output, /6桁:|[█▀▄]{10}/);
  const cols = Number(size[1]), rows = Number(size[2]);
  term.terminal.resize(cols, rows);
  await until(() => term.output.includes('6桁:'));
  const marker = '\x1b[2J\x1b[H';
  assert.ok(term.output.includes(marker));
  const qr = term.output.split(marker).at(-1).split(/\r?\n/).slice(0, rows - 4);
  assert.equal(qr.length, rows - 4);
  assert.equal(qr[0], '▄'.repeat(cols - 2));
  for (const line of qr.slice(1)) {
    assert.equal(line.length, cols - 2);
    assert.match(line, /^█[█▀▄ ]+█$/);
  }
  term.kill('SIGINT');
  assert.equal((await term.result).exitCode, 130);
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });
});

test('サイズ変更待ちの Esc はロックを解放する', async t => {
  const f = await fixture(t);
  const term = f.open(80, 24);
  await until(() => term.output.includes('中止: Esc / Ctrl-C'));
  term.terminal.write('\x1b');
  assert.equal((await term.result).exitCode, 130);
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });
});

test('B8 確定後の読込に失敗すると paired を送らず、PC の pair は失敗し、追加した端末が残らない', async t => {
  const f = await fixture(t);
  let failedDevice;
  const hub = await f.startDaemon({
    async loadDevices(dir) {
      const devices = await loadDevices(dir);
      if (!devices.length || failedDevice) return devices;
      const file = path.join(dir, 'devices.json');
      failedDevice = devices[0].device;
      // 一度だけ実際の読込エラーを起こし、後続の revoke には正しい権限を戻す。
      await fs.chmod(file, 0o644);
      try { return await loadDevices(dir); }
      finally { await fs.chmod(file, 0o600); }
    },
  });
  const term = f.open();
  await until(() => term.output.includes('6桁:'));
  const pairing = hub.pairing;
  const app = new FakeApp(f.relay, 'pair', pairing.id, hub.noiseKey.publicKey);
  assert.ok(await app.handshake());
  app.sendJson(app.pairMessage(pairing.secret));
  await until(() => pairing.candidates.length === 1);
  f.relay.frames = [];

  term.terminal.write(`${app.sas(pairing.secret)}\r`);
  assert.equal((await term.result).exitCode, 1);
  assert.match(term.output, /登録に失敗しました: 登録を反映できませんでした/);
  assert.doesNotMatch(term.output, /登録しました/);
  assert.ok(failedDevice);
  assert.deepEqual(await loadDevices(f.configDir), []);
  assert.deepEqual(hub.devices, []);
  await until(() => app.conn.closed);
  assert.ok(!f.relay.frames.some(frame => frame[0] === 0x02 && frame.subarray(1, 17).equals(app.conn.key)));
  await assert.rejects(fs.stat(f.lockPath), { code: 'ENOENT' });
});
