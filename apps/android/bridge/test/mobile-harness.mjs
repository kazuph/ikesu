// 試験用のにせの中継（2.2 の枠を話す）と、にせのアプリ（Noise の initiator）。
import { createHmac, generateKeyPairSync, randomBytes, sign } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import Noise from 'noise-handshake';
import Cipher from 'noise-handshake/cipher.js';
import dh from 'noise-handshake/dh.js';
import { WebSocketServer } from 'ws';
import { Assembler, encodeMessage, prologueFor } from '../noise-channel.mjs';
import { b64u } from '../devices.mjs';
import { sasCode, pairProof } from '../pairing.mjs';

export const tmpDir = () => mkdtemp(path.join(os.tmpdir(), 'ikesu-mobile-'));
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
export async function until(check, ms = 3000) {
  const end = Date.now() + ms;
  for (;;) { const value = await check(); if (value) return value; if (Date.now() > end) throw Error('until: 時間切れ'); await sleep(5); }
}

export class FakeRelay {
  static async start(relayKey, room) {
    const relay = new FakeRelay(relayKey, room);
    relay.server = new WebSocketServer({ host: '127.0.0.1', port: 0 });
    await once(relay.server, 'listening');
    relay.url = `ws://127.0.0.1:${relay.server.address().port}`;
    relay.server.on('connection', (ws, request) => {
      const expected = createHmac('sha256', relayKey).update('bridge').update(Buffer.from(room, 'base64url')).digest('hex');
      relay.bridgeAuth.push(request.headers.authorization);
      if (request.headers.authorization !== `IKESU-RELAY ${expected}` || request.url !== `/v1/bridge/${room}`) { ws.close(); return; }
      relay.bridge = ws;
      ws.on('message', bytes => relay.fromBridge(Buffer.from(bytes)));
    });
    return relay;
  }
  constructor(relayKey, room) { Object.assign(this, { relayKey, room, apps: new Map(), bridgeAuth: [], bridge: null }); }
  async bridgeUp() { await until(() => this.bridge?.readyState === 1); }
  fromBridge(frame) {
    this.frames?.push(frame);
    const app = this.apps.get(frame.subarray(1, 17).toString('hex'));
    if (!app) return;
    if (frame[0] === 0x02) app.inbox.push(frame.subarray(17));
    else if (frame[0] === 0x03) app.closed = true;
  }
  open(role, id16) {
    const key = randomBytes(16);
    const app = { key, inbox: [], closed: false };
    this.apps.set(key.toString('hex'), app);
    this.bridge.send(Buffer.concat([Buffer.from([0x01]), key, Buffer.from([role === 'app' ? 1 : 2]), id16]));
    app.send = data => this.bridge.send(Buffer.concat([Buffer.from([0x02]), key, data]));
    app.close = () => this.bridge.send(Buffer.concat([Buffer.from([0x03]), key]));
    return app;
  }
  async stop() { for (const ws of this.server.clients) ws.terminate(); await new Promise(resolve => this.server.close(resolve)); }
}

export class FakeApp {
  constructor(relay, role, id16, bridgePublic, { noiseKey = dh.generateKeyPair(), operateKey = generateKeyPairSync('ec', { namedCurve: 'prime256v1' }) } = {}) {
    Object.assign(this, { relay, role, id16, bridgePublic, noiseKey, operateKey });
    this.conn = null; this.assembler = new Assembler(); this.nextId = 1;
  }
  get operatePub() { return b64u(this.operateKey.publicKey.export({ type: 'spki', format: 'der' })); }
  // 握手を行う。返事が来なければ false（bridge が黙って切ったとき）。
  async handshake({ tamperFirst = false } = {}) {
    this.conn = this.relay.open(this.role, this.id16);
    this.noise = new Noise('IK', true, this.noiseKey);
    this.noise.initialise(prologueFor(Buffer.from(this.relay.room, 'base64url')), this.bridgePublic);
    const first = Buffer.from(this.noise.send());
    if (tamperFirst) first[first.length - 1] ^= 1;
    this.conn.send(first);
    for (let i = 0; i < 100; i++) {
      if (this.conn.inbox.length) break;
      if (this.conn.closed) return false;
      await sleep(10);
    }
    if (!this.conn.inbox.length) return false;
    this.noise.recv(this.conn.inbox.shift());
    this.tx = new Cipher(this.noise.tx); this.rx = new Cipher(this.noise.rx);
    this.hash = Buffer.from(this.noise.hash);
    return true;
  }
  sendJson(value) { for (const part of encodeMessage(value, () => this.nextId++)) this.conn.send(Buffer.from(this.tx.encrypt(part))); }
  // 届いた1通ずつを返す。条件に合うまで読み進める。
  async recv(predicate = () => true, ms = 3000) {
    const end = Date.now() + ms;
    for (;;) {
      while (this.conn.inbox.length) {
        const message = this.assembler.push(Buffer.from(this.rx.decrypt(this.conn.inbox.shift())));
        if (message !== undefined && predicate(message)) return message;
      }
      if (Date.now() > end) throw Error('recv: 時間切れ');
      await sleep(5);
    }
  }
  // 約束の形: hashes には自分の h と、ほかの PC の h を入れてよい。
  operateMessage(others = [], { includeOwn = true } = {}) {
    const list = [...(includeOwn ? [this.hash] : []), ...others];
    const sig = b64u(sign('sha256', Buffer.concat([Buffer.from('ikesu-operate/1'), Buffer.from([list.length]), ...list]), { key: this.operateKey.privateKey, dsaEncoding: 'der' }));
    return { type: 'operate', hashes: list.map(item => b64u(item)), sig };
  }
  pairMessage(secret, name = 'テスト端末') { return { type: 'pair', proof: pairProof(secret, this.hash), name, operate: this.operatePub }; }
  sas(secret) { return sasCode(secret, this.hash); }
}
