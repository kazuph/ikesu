// 中継への外向きの接続（PROTOCOL.md 2.1・2.2）。中継は暗号文を渡すだけ。
import { createHmac } from 'node:crypto';
import { WebSocket } from 'ws';
import { fromB64u } from './devices.mjs';

export const relayTag = (relayKey, label, ...parts) => createHmac('sha256', relayKey).update(label).update(Buffer.concat(parts)).digest('hex');
export const expBytes = exp => { const bytes = Buffer.alloc(8); bytes.writeBigUInt64BE(BigInt(exp)); return bytes; };
export const appTag = (relay, device16) => relayTag(relay.relayKey, 'app', fromB64u(relay.room, 32), device16);
export const pairTag = (relay, pairing16, exp) => relayTag(relay.relayKey, 'pair', fromB64u(relay.room, 32), pairing16, expBytes(exp));

const OPEN = 0x01, DATA = 0x02, CLOSE = 0x03;
export const ALLOW = 0x04;
export const MAX_ALLOW = 4095;
export const MAX_FRAME = 65535;

export class RelayClient {
  // pingMs: 黙っている間も、この間隔で ping を送る。前の ping に pong が来ていなければ切ってつなぎ直す
  // （途中の機器が黙った接続を捨てても、送るまで気づけないため）。
  constructor({ relay, onOpen, onData, onClose, log = () => {}, minDelay = 1000, maxDelay = 30000, pingMs = 20000 }) {
    Object.assign(this, { relay, onOpen, onData, onClose, log, minDelay, maxDelay, pingMs });
    this.ws = null; this.stopped = false; this.delay = minDelay; this.timer = null;
  }
  start() { this.stopped = false; this.connect(); }
  connect() {
    if (this.stopped) return;
    const tag = relayTag(this.relay.relayKey, 'bridge', fromB64u(this.relay.room, 32));
    const ws = new WebSocket(`${this.relay.url}/v1/bridge/${this.relay.room}`, { headers: { Authorization: `IKESU-RELAY ${tag}` }, maxPayload: MAX_FRAME + 17, perMessageDeflate: false });
    this.ws = ws;
    let waiting = false, pinger = null;
    ws.on('open', () => {
      if (this.allowFrame) ws.send(this.allowFrame);
      this.delay = this.minDelay; this.log('中継につながりました');
      pinger = setInterval(() => {
        if (waiting) { this.log('中継から pong が来ないので、つなぎ直します'); ws.terminate(); return; }
        waiting = true; ws.ping();
      }, this.pingMs);
    });
    ws.on('pong', () => { waiting = false; });
    ws.on('message', (bytes, binary) => {
      if (!binary || bytes.length < 17) return;
      const kind = bytes[0], connection = bytes.subarray(1, 17), key = Buffer.from(connection).toString('hex');
      try {
        if (kind === OPEN && bytes.length === 17 + 1 + 16 && (bytes[17] === 1 || bytes[17] === 2)) this.onOpen(key, bytes[17] === 1 ? 'app' : 'pair', Buffer.from(bytes.subarray(18)));
        else if (kind === DATA) this.onData(key, Buffer.from(bytes.subarray(17)));
        else if (kind === CLOSE && bytes.length === 17) this.onClose(key);
      } catch (error) { this.log(`枠の処理に失敗: ${error.message}`); this.closeConn(key); }
    });
    ws.on('error', error => this.log(`中継のエラー: ${error.message}`));
    ws.on('close', () => {
      clearInterval(pinger);
      if (this.ws === ws) this.ws = null;
      this.onReset?.();
      if (this.stopped) return;
      this.timer = setTimeout(() => { this.timer = null; this.connect(); }, this.delay);
      this.delay = Math.min(this.delay * 2, this.maxDelay);
    });
  }
  setAllow(ids) {
    if (ids.length > MAX_ALLOW) {
      this.log('登録端末が多すぎて中継へ一覧を送れません。中継ではすべての端末を断ります');
      ids = [];
    }
    const header = Buffer.alloc(17);
    header[0] = ALLOW;
    this.allowFrame = Buffer.concat([header, ...ids]);
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(this.allowFrame);
  }
  frame(kind, key, body = Buffer.alloc(0)) { return Buffer.concat([Buffer.from([kind]), Buffer.from(key, 'hex'), body]); }
  sendData(key, buffer) {
    if (buffer.length > MAX_FRAME) throw Error('1通が 65,535 バイトを超えています');
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(this.frame(DATA, key, buffer));
  }
  // 送信待ちが limit を下回るまで待つ（動画のように大きなものを積み上げないため）。
  async drain(limit = 1024 * 1024) {
    while (this.ws?.readyState === WebSocket.OPEN && this.ws.bufferedAmount > limit) await new Promise(resolve => setTimeout(resolve, 10));
  }
  closeConn(key) { if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(this.frame(CLOSE, key)); }
  stop() {
    this.stopped = true; clearTimeout(this.timer);
    this.ws?.terminate(); this.ws = null;
  }
}
