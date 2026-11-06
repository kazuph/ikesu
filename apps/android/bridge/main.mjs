#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { acquireDaemonLock, createGetDecision, createRunAnswer, ensureNativeDialog, spawnDecisionWatch } from '../../mac-dialog-decisions/mac-dialog-decisions.mjs';
import { ClientViews } from '../../mac-dialog-decisions/client-views.mjs';
import { RichService } from '../../mac-dialog-decisions/rich-service.mjs';
import { ownerAlive, ownerProven, readOwner } from '../../mac-dialog-decisions/lifecycle.mjs';
import { DecisionRegistry } from './registry.mjs';
import { loadToken, rotateToken, tailscaleAddress } from './security.mjs';
import { listen } from './socket.mjs';

const exec = promisify(execFile);
const root = path.dirname(fileURLToPath(import.meta.url));
const pluginId = 'kazuph.android';
const configDir = process.env.IKESU_PLUGIN_CONFIG_DIR || path.join(os.homedir(), '.config/ikesu/plugins/config', pluginId);
const stateDir = process.env.IKESU_PLUGIN_STATE_DIR || path.join(os.homedir(), '.local/state/ikesu/plugins', pluginId);
let file = {};
try { file = JSON.parse(fs.readFileSync(path.join(configDir, 'config.json'), 'utf8')); }
catch (error) { if (error.code !== 'ENOENT') throw error; }
const config = { pluginId, configDir, stateDir, enabled: file.enabled !== false,
  ikesuBin: process.env.IKESU_BIN_PATH || file.ikesu_bin || 'ikesu',
  port: file.port ?? 8776, laterMs: (file.later_minutes ?? 10) * 60000,
  tailscale: file.tailscale_path || 'tailscale' };
if (!Number.isInteger(config.port) || config.port < 1 || config.port > 65535 || !Number.isFinite(config.laterMs) || config.laterMs < 0) throw Error('設定値が不正です');
const log = message => process.stderr.write(`[android-decisions] ${message}\n`);

async function ensure() {
  fs.mkdirSync(stateDir, { recursive: true, mode: 0o700 });
  const lock = path.join(stateDir, 'ensure.lock');
  try { fs.mkdirSync(lock, { mode: 0o700 }); }
  catch (error) {
    if (error.code !== 'EEXIST') throw error;
    try { if (Date.now() - fs.statSync(lock).mtimeMs <= 60_000) return; fs.rmdirSync(lock); fs.mkdirSync(lock, { mode: 0o700 }); }
    catch (failure) { if (failure.code === 'ENOENT' || failure.code === 'EEXIST') return; throw failure; }
  }
  try {
    const owner = readOwner(stateDir);
    if (ownerAlive(owner)) {
      if (!ownerProven(owner, stateDir)) log('稼働中プロセスの所有を確認できないため起動しません');
      return;
    }
    const starting = path.join(stateDir, 'starting.json');
    try { if (Date.now() - JSON.parse(fs.readFileSync(starting, 'utf8')).started < 30000) return; } catch {}
    const vars = ['HOME', 'PATH', 'IKESU_SOCKET_PATH', 'IKESU_SOCKET_PATH_EXPLICIT', 'IKESU_CONFIG_PATH', 'IKESU_BIN_PATH'].filter(key => process.env[key]).map(key => `${key}=${process.env[key]}`);
    vars.push(`IKESU_PLUGIN_CONFIG_DIR=${configDir}`, `IKESU_PLUGIN_STATE_DIR=${stateDir}`);
    await exec(config.ikesuBin, ['run', '--label', 'android-decisions', '--cwd', root, '--completion', 'none', '--', 'env', ...vars, process.execPath, path.join(root, 'main.mjs'), 'run']);
    fs.writeFileSync(starting, JSON.stringify({ started: Date.now() }), { mode: 0o600 });
  } finally { fs.rmdirSync(lock); }
}

async function run() {
  fs.mkdirSync(stateDir, { recursive: true, mode: 0o700 });
  const nativeDialog = ensureNativeDialog(config, log);
  if (!nativeDialog) throw Error('画像の検査に使う Mac ダイアログを準備できません');
  const lock = await acquireDaemonLock(nativeDialog, stateDir);
  if (!lock) return;
  fs.writeFileSync(path.join(stateDir, 'daemon.json'), JSON.stringify({ pid: process.pid, stateDir: fs.realpathSync(stateDir), script: fileURLToPath(import.meta.url), node: process.execPath }), { mode: 0o600 });
  const address = await tailscaleAddress(config.tailscale);
  let token = await loadToken(configDir);
  const clientViews = new ClientViews();
  const rich = new RichService({ ...config, nativeDialog });
  const daemon = new DecisionRegistry({ config, clientViews, rich,
    spawnWatch: () => spawnDecisionWatch(config, log),
    runAnswer: createRunAnswer(config, log), getDecision: createGetDecision(config, log), log });
  const socket = await listen({ address, port: config.port, token,
    snapshot: () => daemon.snapshot(), answer: value => daemon.answer(value), later: id => daemon.later(id),
    videoPath: (id, key) => daemon.videoPath(id, key) });
  daemon.onSnapshot = value => socket.publish(value);
  await daemon.start();
  const watcher = fs.watch(configDir, async (_event, name) => {
    if (name !== 'token') return;
    socket.setToken(null);
    try { token = await loadToken(configDir); socket.setToken(token); }
    catch (error) { log(`合言葉の再読込に失敗: ${error.message}`); }
  });
  const stop = async () => { watcher.close(); await socket.close(); await daemon.stop(); await lock.close(); };
  for (const signal of ['SIGTERM', 'SIGINT']) process.once(signal, () => { void stop().then(() => process.exit(0)); });
  log(`Tailscale ${address}:${config.port} で待ち受けています`);
}

const mode = process.argv[2];
if (mode === 'rotate-token') await rotateToken(configDir);
else if (process.platform !== 'darwin') throw Error('bridge は macOS 専用です');
else if (!config.enabled) log('設定で無効化されています');
else if (mode === 'ensure') await ensure();
else if (mode === 'run') await run();
else throw Error('usage: main.mjs ensure|run|rotate-token');
