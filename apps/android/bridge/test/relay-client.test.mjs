// The bridge's relay connection: keepalive pings keep a healthy link, and a
// link that stops answering pings is dropped and reconnected.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { WebSocketServer } from 'ws';
import { RelayClient } from '../relay-client.mjs';
import { b64u } from '../devices.mjs';

async function relayServer({ autoPong }) {
  const server = new WebSocketServer({ port: 0, host: '127.0.0.1', autoPong });
  await new Promise(resolve => server.once('listening', resolve));
  const connections = [];
  server.on('connection', ws => connections.push(ws));
  const relay = { url: `ws://127.0.0.1:${server.address().port}`, room: b64u(randomBytes(32)), relayKey: randomBytes(32) };
  return { server, connections, relay };
}

const until = async (check, ms = 5000) => {
  const deadline = Date.now() + ms;
  while (!check()) { if (Date.now() > deadline) throw Error('timeout'); await new Promise(r => setTimeout(r, 10)); }
};

test('a relay that answers pings keeps one connection', async () => {
  const { server, connections, relay } = await relayServer({ autoPong: true });
  const logs = [];
  // 150ms: the other test files run at the same time, and a busy machine can take over 40ms to answer a local ping.
  const client = new RelayClient({ relay, onOpen() {}, onData() {}, onClose() {}, log: m => logs.push(m), pingMs: 150, minDelay: 20 });
  client.start();
  try {
    await new Promise(r => setTimeout(r, 650));
    assert.equal(connections.length, 1);
    assert.ok(!logs.some(m => m.includes('pong')), logs.join('\n'));
  } finally { client.stop(); server.close(); }
});

test('a relay that stops answering pings is dropped and reconnected', async () => {
  const { server, connections, relay } = await relayServer({ autoPong: false });
  const logs = [];
  const client = new RelayClient({ relay, onOpen() {}, onData() {}, onClose() {}, log: m => logs.push(m), pingMs: 40, minDelay: 20 });
  client.start();
  try {
    await until(() => connections.length >= 2);
    assert.ok(logs.some(m => m.includes('pong')), logs.join('\n'));
  } finally { client.stop(); server.close(); }
});

test('B1 ALLOW は接続直後と再接続の先頭、OPEN 中は即送信し、超過時は空の一覧を送る', async () => {
  const { server, connections, relay } = await relayServer({ autoPong: true });
  const received = [];
  server.on('connection', ws => {
    const frames = [];
    received.push(frames);
    ws.on('message', bytes => frames.push(bytes));
  });
  const logs = [];
  const client = new RelayClient({ relay, onOpen() {}, onData() {}, onClose() {}, log: m => logs.push(m), minDelay: 20 });
  const a = randomBytes(16), b = randomBytes(16);
  const header = Buffer.alloc(17);
  header[0] = 0x04;
  const full = Buffer.concat([header, a, b]);
  client.setAllow([a, b]);
  client.start();
  try {
    await until(() => received[0]?.length);
    assert.deepEqual(received[0][0], full);

    connections[0].terminate();
    await until(() => received[1]?.length);
    assert.deepEqual(received[1][0], full);

    client.setAllow([b]);
    await until(() => received[1].length >= 2);
    assert.deepEqual(received[1][1], Buffer.concat([header, b]));
    assert.equal(connections.length, 2);

    client.setAllow(Array(4096).fill(a));
    await until(() => received[1].length >= 3);
    assert.deepEqual(received[1][2], header);
    assert.ok(logs.includes('登録端末が多すぎて中継へ一覧を送れません。中継ではすべての端末を断ります'));
  } finally { client.stop(); server.close(); }
});

test('B2 CLOSE 16・17・18バイトのうち17バイトだけを処理する', async () => {
  const { server, connections, relay } = await relayServer({ autoPong: true });
  const closed = [];
  let receivedData = false;
  const client = new RelayClient({ relay, onOpen() {}, onData() { receivedData = true; }, onClose: key => closed.push(key) });
  const id = randomBytes(16);
  const close = Buffer.concat([Buffer.from([0x03]), id]);
  client.start();
  try {
    await until(() => connections.length);
    const ws = connections[0];
    ws.send(close.subarray(0, 16));
    ws.send(close);
    ws.send(Buffer.concat([close, Buffer.of(0)]));
    ws.send(Buffer.concat([Buffer.of(0x02), id, Buffer.of(1)]));
    await until(() => receivedData);
    assert.deepEqual(closed, [id.toString('hex')]);
  } finally { client.stop(); server.close(); }
});
