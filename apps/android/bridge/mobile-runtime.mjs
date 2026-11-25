// run に中継の経路を足す部品。relay.json があるときだけ使う。
import fs from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { loadDevices, loadNoiseKey, loadRelay, PERMISSIONS } from './devices.mjs';
import { MobileHub } from './mobile-hub.mjs';
import { RelayClient } from './relay-client.mjs';

export async function startMobile({ configDir, stateDir, decisions, log, control = true, pollMs = 1000 }) {
  const relay = await loadRelay(configDir);
  if (!relay) return null;
  const noiseKey = await loadNoiseKey(configDir);
  const hub = new MobileHub({ configDir, noiseKey, relay, log, transport: { sendData: () => {}, closeConn: () => {} }, ...decisions });
  const client = new RelayClient({ relay, log, onOpen: (...args) => hub.onOpen(...args), onData: (...args) => hub.onData(...args), onClose: key => hub.onClose(key) });
  client.onReset = () => hub.onReset();
  hub.transport = client;
  hub.onDevices = devices => client.setAllow(devices.map(device => Buffer.from(device.device, 'base64url')));
  const reload = () => hub.reloadDevices();
  let watcher = null;
  try { watcher = fs.watch(configDir, (_event, name) => { if (name === 'devices.json') void reload(); }); } catch { /* 下の定期確認で補う */ }
  await reload();
  const devicesFile = path.join(configDir, 'devices.json');
  let lastVersion;
  const poll = setInterval(() => {
    let version = 'missing';
    try {
      const { mtimeMs, ino, size } = fs.statSync(devicesFile);
      version = `${mtimeMs}:${ino}:${size}`;
    } catch { /* 読めない場合も reload で確認する */ }
    if (version !== lastVersion) {
      lastVersion = version;
      void reload();
    }
  }, pollMs);
  poll.unref?.();
  client.start();
  const server = control ? await listenControl(path.join(stateDir, 'pair.sock'), hub) : null;
  return { hub, client, relay, async stop() { clearInterval(poll); watcher?.close(); client.stop(); hub.stop(); await server?.close(); } };
}

// ---- pair コマンドが run 中の bridge と話すための、手元だけの unix socket ----
async function listenControl(file, hub) {
  fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
  if (await connect(file).then(socket => { socket.destroy(); return true; }, () => false)) return null;
  try { fs.unlinkSync(file); } catch { /* 無ければよい */ }
  const server = net.createServer(socket => {
    let buffer = '';
    let ownedPairing = null;
    const write = value => { if (!socket.destroyed) socket.write(`${JSON.stringify(value)}\n`); };
    let candidateHandler;
    socket.on('close', () => {
      if (hub.pairing === ownedPairing && ownedPairing) hub.closePairing();
      if (candidateHandler && hub.onCandidate === candidateHandler) hub.onCandidate = null;
    });
    socket.on('error', () => {});
    socket.on('data', async chunk => {
      buffer += chunk;
      let index;
      while ((index = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, index); buffer = buffer.slice(index + 1);
        try {
          const message = JSON.parse(line);
          if (message.op === 'pair_start') {
            if (!Array.isArray(message.permissions) || !message.permissions.every(item => PERMISSIONS.includes(item)) || typeof message.name !== 'string' || !message.name) throw Error('引数が不正です');
            if (hub.pairing?.active) throw Error('既にペアリングが実行中です');
            ownedPairing = hub.startPairing({ name: message.name, permissions: message.permissions });
            candidateHandler = candidate => { if (hub.pairing === ownedPairing) write({ event: 'candidate', name: candidate.name }); };
            hub.onCandidate = candidateHandler;
            write({ ok: true, qr: ownedPairing.qr() });
          } else if (message.op === 'pair_confirm') {
            if (!ownedPairing || hub.pairing !== ownedPairing) throw Error('この接続のペアリングはありません');
            const device = await hub.confirmPairing(message.code);
            write({ ok: Boolean(device), device: device?.device, active: Boolean(hub.pairing?.active) });
          } else if (message.op === 'pair_cancel') { if (ownedPairing && hub.pairing === ownedPairing) hub.closePairing(); ownedPairing = null; write({ ok: true }); }
          else throw Error('不明な操作です');
        } catch (error) { write({ ok: false, error: error.message }); }
      }
    });
  });
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(file, resolve); });
  fs.chmodSync(file, 0o600);
  return { close: () => new Promise(resolve => server.close(() => { try { fs.unlinkSync(file); } catch { /* 済み */ } resolve(); })) };
}

export function connect(file) {
  return new Promise((resolve, reject) => { const socket = net.connect(file); socket.once('connect', () => resolve(socket)); socket.once('error', reject); });
}

// run 中の bridge へつなぐ。無ければ null。
export async function remotePairing(stateDir) {
  let socket;
  try { socket = await connect(path.join(stateDir, 'pair.sock')); } catch { return null; }
  const waiting = []; const listeners = []; let buffer = '';
  socket.on('data', chunk => {
    buffer += chunk;
    let index;
    while ((index = buffer.indexOf('\n')) >= 0) {
      const message = JSON.parse(buffer.slice(0, index)); buffer = buffer.slice(index + 1);
      if (message.event === 'candidate') listeners.forEach(listener => listener(message)); else waiting.shift()?.(message);
    }
  });
  socket.on('close', () => waiting.splice(0).forEach(resolve => resolve({ ok: false, error: '接続が切れました' })));
  const call = value => new Promise(resolve => { waiting.push(resolve); socket.write(`${JSON.stringify(value)}\n`); });
  return {
    async start(options) { const reply = await call({ op: 'pair_start', ...options }); if (!reply.ok) throw Error(reply.error); return reply.qr; },
    async confirm(code) {
      const reply = await call({ op: 'pair_confirm', code });
      if (reply.error) throw Error(reply.error);
      return reply.ok ? { device: reply.device } : { device: null, active: reply.active };
    },
    onCandidate(listener) { listeners.push(listener); },
    async cancel() { await call({ op: 'pair_cancel' }); },
    close() { socket.destroy(); },
  };
}

export function localPairing(mobile) {
  const hub = mobile.hub;
  return {
    async start(options) { return hub.startPairing(options).qr(); },
    async confirm(code) { const device = await hub.confirmPairing(code); return device ? { device: device.device } : { device: null, active: Boolean(hub.pairing?.active) }; },
    onCandidate(listener) { hub.onCandidate = candidate => listener({ name: candidate.name }); },
    async cancel() { hub.closePairing(); },
    close() {},
  };
}
export { loadDevices };
