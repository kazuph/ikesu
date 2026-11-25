// 中継から届く接続を受け持つ。握手・ペアリング・通知・操作（PROTOCOL.md 3〜3.5・5）。
import path from 'node:path';
import { createReadStream } from 'node:fs';
import { randomBytes } from 'node:crypto';
import { Assembler, encodeMessage, NoiseResponder } from './noise-channel.mjs';
import { addDevice, b64u, loadDevices } from './devices.mjs';
import { constantEqual, Pairing, verifyOperate } from './pairing.mjs';
import { appTag } from './relay-client.mjs';
import { validateAnswer, validateShown } from './messages.mjs';
import { MAX_FILE_BYTES, Screen, saveIncomingFile, serverAlive, shellQuote, validSize } from './screen.mjs';

const MAX_SCREEN_INPUT = 64 * 1024;

const ONLY = (object, keys) => object && typeof object === 'object' && !Array.isArray(object) && Object.keys(object).every(key => keys.includes(key));
const HANDSHAKE_WINDOW_MS = 60_000;
const HANDSHAKE_FAILURES_PER_WINDOW = 10;
const FAILURE_IDS_KEPT = 256;
const MAX_SESSIONS = 16;

class Session {
  constructor(hub, key, role, id) {
    Object.assign(this, { hub, key, role, id, closed: false, ready: false, operating: false, beatN: 0, device: null, sentVideos: new Set(), assembler: new Assembler(), chunkBeats: new Map() });
    this.noise = new NoiseResponder({ staticKey: hub.noiseKey, room: hub.roomBytes, now: hub.now });
    this.handshakeTimer = setTimeout(() => this.close(), hub.handshakeMs); this.handshakeTimer.unref?.();
  }
  close(failedHandshake = false, notifyRelay = true) {
    if (this.closed) return;
    this.closed = true;
    if (failedHandshake) this.hub.noteHandshakeFailure(this.id);
    clearTimeout(this.handshakeTimer); this.stopOperating(false); this.closeScreen(false);
    if (this.slot) this.hub.pairing?.release(this.slot);
    if (this.candidate) this.hub.pairing?.dropCandidate(this.candidate);
    // A newer connection under the same id (see onOpen) stays registered.
    if (this.hub.sessions.get(this.key) === this) this.hub.sessions.delete(this.key);
    if (notifyRelay) this.hub.transport.closeConn(this.key);
  }
  send(value) {
    if (this.closed || !this.ready) return;
    if (this.noise.needsRenewal) { this.close(); return; }
    try { for (const part of encodeMessage(value, () => (this.messageId = ((this.messageId ?? 0) + 1) >>> 0))) this.hub.transport.sendData(this.key, this.noise.encrypt(part)); }
    catch (error) { this.hub.log(`送信に失敗: ${error.message}`); this.close(); }
  }
  async onData(buffer) {
    if (this.closed) return;
    if (!this.ready) { this.handshake(buffer); return; }
    let message;
    try {
      const plain = this.noise.decrypt(buffer);
      // 分割したメッセージは、顔・指紋で開いた後の回答（画像つき）だけが使う。開く前は溜めさせない。
      if (plain[0] === 0x01 && !(this.role === 'app' && this.operating)) { this.close(); return; }
      // 分割したメッセージごとに、最初の1通が届いたときの心拍を覚える。ファイルと回答の画像は届き終えるまでに心拍が進むので、
      // それだけは送り始めの時点で比べる（キー入力は届き終えた時点で比べる。中継が途中で止めた古い入力を打たないため）。
      const chunkId = plain[0] === 0x01 && plain.length >= 6 ? plain.readUInt32BE(1) : null;
      if (chunkId !== null && !this.chunkBeats.has(chunkId)) this.chunkBeats.set(chunkId, this.beatN);
      message = this.assembler.push(plain);
      this.messageBeat = this.beatN;
      this.startBeat = this.beatN;
      if (chunkId !== null && message !== undefined) { this.startBeat = this.chunkBeats.get(chunkId); this.chunkBeats.delete(chunkId); }
    } catch { this.close(); return; }
    if (message === undefined) return;
    try { await (this.role === 'pair' ? this.onPair(message) : this.onApp(message)); }
    catch (error) { this.hub.log(`処理に失敗: ${error.message}`); this.close(); }
  }
  handshake(buffer) {
    try {
      const remote = this.noise.receiveFirst(buffer);
      if (this.role === 'app') {
        this.device = this.hub.devices.find(device => device.device === b64u(this.id));
        if (!this.device || !constantEqual(this.device.noise_pub, b64u(remote))) { this.close(true); return; }
      } else {
        this.slot = this.hub.pairing?.matches(this.id) ? this.hub.pairing.reserve() : null;
        if (!this.slot) { this.close(true); return; }
      }
      const reply = this.noise.reply();
      this.hub.transport.sendData(this.key, reply);
      this.ready = true; clearTimeout(this.handshakeTimer);
      this.remote = remote;
      if (this.role === 'app') { this.hub.log(`${this.device.name} が接続しました`); this.sendNotice(); }
    } catch { this.close(true); }
  }
  async onPair(message) {
    if (this.paired || this.candidate || !ONLY(message, ['type', 'proof', 'name', 'operate']) || message.type !== 'pair') { this.close(); return; }
    const pairing = this.hub.pairing;
    const result = pairing?.submitProof(this.slot, { h: this.noise.hash, proof: message.proof, name: message.name, operate: message.operate, noisePublic: this.remote });
    if (!result || result.error) {
      if (result?.error === 'proof') this.hub.log('ペアリングの proof が合いません');
      if (result?.error === 'proof' && !pairing.active) this.hub.log('外れが続いたため、この QR を無効にしました');
      this.close(); return;
    }
    this.candidate = result.candidate; this.candidate.session = this;
    this.hub.log(`ペアリング候補が来ました（${this.candidate.name}）。PC の端末に 6 桁を入力してください`);
    this.hub.onCandidate?.(this.candidate);
  }
  // ---- ふだんの接続 ----
  can(permission) { return this.device.permissions.includes(permission); }
  async sendNotice() {
    if (!this.ready || this.closed || !this.can('notify')) return;
    try {
      const decisions = (await this.hub.snapshot()).map(item => ({ id: item.id, title: item.raw_title ?? item.title ?? '', origin: item.location ?? '',
        created_unix_ms: this.hub.createdOf?.(item.id) ?? item.created_unix_ms ?? 0, notify: item.notify !== false }));
      this.send({ type: 'notice', decisions });
    } catch (error) { this.hub.log(`通知の作成に失敗: ${error.message}`); }
  }
  async sendSnapshot() {
    if (!this.operating) return;
    const decisions = await this.hub.snapshot();
    if (!this.operating) return;
    this.send({ type: 'snapshot', decisions });
    void this.sendVideos(decisions);
  }
  async sendVideos(decisions) {
    for (const decision of decisions) for (const video of decision.videos || []) {
      if (!video || this.sentVideos.has(video.key)) continue;
      const file = this.hub.videoPath(decision.id, video.key);
      if (!file) continue;
      this.sentVideos.add(video.key);
      try {
        let index = 0;
        for await (const bytes of createReadStream(file, { highWaterMark: 32 * 1024 })) {
          if (!this.operating) return;
          this.send({ type: 'video_chunk', id: decision.id, key: video.key, index: index++, data: bytes.toString('base64') });
          await this.hub.transport.drain?.();
        }
        if (this.operating) this.send({ type: 'video_end', id: decision.id, key: video.key });
      } catch { this.sentVideos.delete(video.key); }
    }
  }
  startOperating() {
    this.stopOperating(false);
    this.operating = true; this.touch();
    this.send({ type: 'operate_ok', until: this.until });
    this.beatTimer = setInterval(() => this.send({ type: 'beat', n: ++this.beatN }), this.hub.beatMs);
    this.beatTimer.unref?.();
    void this.sendSnapshot().catch(() => this.close());
  }
  touch() {
    clearTimeout(this.idleTimer);
    this.until = this.hub.now() + this.hub.idleMs;
    this.idleTimer = setTimeout(() => this.stopOperating(true), this.hub.idleMs); this.idleTimer.unref?.();
  }
  stopOperating(notify) {
    clearTimeout(this.idleTimer); clearInterval(this.beatTimer);
    const was = this.operating;
    this.operating = false; this.sentVideos.clear();
    this.closeScreen(true);
    // 終わった接続は閉じる。同じ握手ハッシュへの古い署名を送り直して開き直すことをさせない。
    // アプリはつなぎ直し、新しい握手ハッシュに顔・指紋で署名し直す。
    if (was && notify) { this.send({ type: 'operate_expired' }); this.close(); }
  }
  fresh(message, fromStart = false) {
    if (message.beat === undefined) return true;
    // まだ送っていない番号の心拍は受けない。
    const at = (fromStart ? this.startBeat : this.messageBeat) ?? this.beatN;
    return Number.isInteger(message.beat) && message.beat <= this.beatN && at - message.beat < 2;
  }
  // ---- 画面（PROTOCOL.md 4章）。見るには screen、打つには input の権限と新しい心拍が要る ----
  closeScreen(notify) {
    const screen = this.screen; if (!screen) return;
    this.screen = null; screen.close();
    if (notify) this.send({ type: 'screen_closed', screen: screen.id });
  }
  async onScreen(message) {
    const type = message.type;
    const deny = text => { this.hub.log(`${this.device.name}: 画面の要求（${type}）を断りました: ${text}`); this.send({ type: 'error', message: text }); };
    if (!this.operating) { deny('operate required'); return; }
    if (type === 'screen_open') {
      if (!ONLY(message, ['type', 'cols', 'rows']) || !validSize(message.cols, message.rows)) { this.close(); return; }
      if (!this.can('screen')) { deny('permission denied'); return; }
      this.closeScreen(true);
      // サーバーの確認は1本ずつ。待っている間に届いた screen_open は、最後の大きさだけを覚えて、確認の後に使う
      // （要求の数だけ確認のプロセスを起こさない。PTY も2つ作らない）。
      if (this.screenChecking) { this.screenWanted = message; return; }
      this.screenChecking = true;
      let alive;
      try { alive = await this.hub.checkServer(); } finally { this.screenChecking = false; }
      if (this.screenWanted) { message = this.screenWanted; this.screenWanted = null; }
      if (!alive) { deny('ikesu のサーバーにつながりません'); return; }
      if (!this.operating || this.closed || !this.can('screen')) return;
      this.closeScreen(true);
      const id = this.hub.nextScreenId++;
      try {
        // スマホごとに、最後に見ていたスペースとタブを覚えるファイル。アプリを離れて戻ったとき、新しい画面がそこから始まる。
        const selectionFile = this.hub.configDir && /^[A-Za-z0-9_-]+$/.test(this.device.device) ? path.join(this.hub.configDir, 'screens', `${this.device.device}.json`) : undefined;
        this.screen = this.hub.openScreen({ id, cols: message.cols, rows: message.rows, selectionFile,
          onData: (seq, bytes) => this.send({ type: 'screen_data', screen: id, seq, data: bytes.toString('base64') }),
          onExit: info => {
            if (!info?.closedByUs) this.hub.log(`${this.device.name}: 画面の ikesu が終わりました（終了コード ${info?.exitCode}、シグナル ${info?.signal}）: ${JSON.stringify(info?.head?.toString('utf8').replace(/\x1b\[[0-9;?]*[A-Za-z]/g, '').slice(0, 200))}`);
            if (this.screen?.id === id) { this.screen = null; this.send({ type: 'screen_closed', screen: id }); }
          } });
      } catch (error) { this.hub.log(`画面を開けません: ${error.message}`); deny('画面を開けません'); return; }
      this.touch();
      this.hub.log(`${this.device.name}: 画面を開きました（${message.cols}×${message.rows}）`);
      this.send({ type: 'screen_opened', screen: id, cols: message.cols, rows: message.rows });
      return;
    }
    const screen = this.screen;
    if (type === 'screen_input') {
      if (!ONLY(message, ['type', 'screen', 'beat', 'data']) || typeof message.data !== 'string' || message.data.length > MAX_SCREEN_INPUT * 2) { this.close(); return; }
      if (!this.can('input')) { deny('permission denied'); return; }
      // 打つときは心拍が必須。古い心拍のキー入力は、遅れて届いたものとして捨てる（PROTOCOL.md 3.5）。
      if (!Number.isInteger(message.beat) || !this.fresh(message)) { deny('stale beat'); return; }
      if (!screen || screen.id !== message.screen) return;
      const bytes = Buffer.from(message.data, 'base64');
      if (bytes.length > MAX_SCREEN_INPUT) { this.close(); return; }
      this.touch(); screen.write(bytes); return;
    }
    if (type === 'screen_file') {
      // スマホのファイルを PC に保存し、そのパスを画面に打つ（ドラッグ＆ドロップと同じ形。Enter は押さない）。
      if (!ONLY(message, ['type', 'screen', 'beat', 'name', 'data']) || typeof message.name !== 'string' || typeof message.data !== 'string' ||
          message.data.length > Math.ceil(MAX_FILE_BYTES / 3) * 4 + 4) { this.close(); return; }
      if (!this.can('input')) { deny('permission denied'); return; }
      if (!Number.isInteger(message.beat) || !this.fresh(message, true)) { deny('stale beat'); return; }
      if (!screen || screen.id !== message.screen) return;
      const bytes = Buffer.from(message.data, 'base64');
      if (bytes.length > MAX_FILE_BYTES) { deny('ファイルが大きすぎます'); return; }
      let file;
      try { file = this.hub.saveFile(message.name, bytes); } catch (error) { deny(`保存できませんでした: ${error.message}`); return; }
      this.hub.log(`${this.device.name}: スマホのファイルを保存しました: ${file}`);
      // 貼り付けの形で打つ。ikesu が prefix やメニューの待ちでも、パスの文字が操作として読まれない。
      this.touch(); screen.write(Buffer.from(`\x1b[200~${shellQuote(file)} \x1b[201~`));
      this.send({ type: 'screen_file_saved', screen: screen.id, path: file });
      return;
    }
    if (type === 'screen_resize') {
      if (!ONLY(message, ['type', 'screen', 'cols', 'rows']) || !validSize(message.cols, message.rows)) { this.close(); return; }
      if (!this.can('screen')) { deny('permission denied'); return; }
      if (screen && screen.id === message.screen) { this.touch(); screen.resize(message.cols, message.rows); }
      return;
    }
    if (type === 'screen_close') {
      if (!ONLY(message, ['type', 'screen'])) { this.close(); return; }
      if (screen && screen.id === message.screen) this.closeScreen(true);
      return;
    }
    this.close();
  }
  async onApp(message) {
    const type = message?.type;
    if (type === 'operate') {
      if (!ONLY(message, ['type', 'hashes', 'sig']) || (!this.can('answer') && !this.can('screen') && !this.can('input')) ||
          !verifyOperate(this.device.operate_pub, this.noise.hash, message.hashes, message.sig)) {
        this.send({ type: 'error', message: 'operate rejected' }); this.close(); return;
      }
      // 操作中に届いた operate では期限を延ばさない（延ばすのは本当の操作だけ）。
      if (this.operating) { this.send({ type: 'operate_ok', until: this.until }); return; }
      this.startOperating(); return;
    }
    if (type === 'shown') {
      await this.hub.shown(validateShown(message).ids); return;
    }
    if (type === 'operate_end') { this.stopOperating(true); return; }
    if (typeof type === 'string' && type.startsWith('screen_')) { await this.onScreen(message); return; }
    if (type !== 'answer' && type !== 'later') { this.close(); return; }
    if (!this.operating) { this.send({ type: 'error', sequence: message?.sequence, message: 'operate required' }); return; }
    if (!this.can('answer')) { this.send({ type: 'error', sequence: message?.sequence, message: 'permission denied' }); return; }
    if (!this.fresh(message, true)) { this.send({ type: 'error', sequence: message.sequence, message: 'stale beat' }); return; }
    const { beat, ...body } = message;
    this.touch();
    try {
      if (type === 'answer') {
        const response = await this.hub.answer(validateAnswer(body));
        this.send({ type: 'answer_result', sequence: body.sequence, ...response });
      } else if (ONLY(body, ['type', 'id']) && typeof body.id === 'string') await this.hub.later(body.id);
      else this.close();
    } catch (error) { this.send({ type: 'error', sequence: body?.sequence, message: error.message }); }
  }
}

export class MobileHub {
  constructor({ configDir, noiseKey, relay, transport, snapshot, answer, later, shown = async () => {}, videoPath = () => null, createdOf,
    now = Date.now, log = () => {}, beatMs = 5000, idleMs = 600_000, handshakeMs = 10_000,
    ikesuBin = 'ikesu', serverAlive: alive, openScreen, saveFile, loadDevices: readDevices = loadDevices }) {
    Object.assign(this, { configDir, noiseKey, relay, room: relay.room, transport, snapshot, answer, later, shown, videoPath, createdOf, now, log, beatMs, idleMs, handshakeMs, readDevices });
    this.nextScreenId = 1;
    this.serverAlive = alive ?? (() => serverAlive(ikesuBin));
    this.openScreen = openScreen ?? (options => new Screen({ ...options, ikesuBin }));
    this.saveFile = saveFile ?? ((name, bytes) => saveIncomingFile(name, bytes));
    this.roomBytes = Buffer.from(relay.room, 'base64url');
    this.sessions = new Map(); this.devices = []; this.pairing = null; this.failures = new Map();
  }
  reloadDevices() {
    this.reloadWanted = true;
    this.reloadRun ??= (async () => {
      try {
        while (this.reloadWanted) {
          this.reloadWanted = false;
          await this.applyDevicesOnce();
        }
      } finally {
        this.reloadRun = null;
      }
    })();
    return this.reloadRun;
  }
  async applyDevicesOnce() {
    let readFailed = false;
    try {
      this.devices = await this.readDevices(this.configDir);
    } catch (error) {
      this.log(`devices.json を読めません。すべての接続を切ります: ${error.message}`);
      this.devices = [];
      readFailed = true;
    }
    this.onDevices?.(this.devices);
    if (readFailed) return this.onReset();
    for (const session of [...this.sessions.values()]) {
      if (session.role !== 'app') continue;
      const current = this.devices.find(device => device.device === session.device?.device);
      // A changed key or a changed set of permissions ends the connection: the app connects and signs again under the new
      // permissions, and a screen opened under the old ones does not stay open.
      const samePermissions = current && JSON.stringify([...current.permissions].sort()) === JSON.stringify([...session.device.permissions].sort());
      if (!current || current.noise_pub !== session.device.noise_pub || current.operate_pub !== session.device.operate_pub || !samePermissions) session.close();
      else session.device = current;
    }
  }
  // 握手の失敗は端末（またはペアリング）の ID ごとに数える。外した端末が繰り返しつないでも、ほかの端末は入れる。
  noteHandshakeFailure(id) {
    const key = b64u(id ?? Buffer.alloc(0));
    const list = this.failures.get(key) ?? [];
    list.push(this.now());
    this.failures.delete(key); this.failures.set(key, list);
    while (this.failures.size > FAILURE_IDS_KEPT) this.failures.delete(this.failures.keys().next().value);
  }
  tooManyFailures(id) {
    const key = b64u(id ?? Buffer.alloc(0));
    const limit = this.now() - HANDSHAKE_WINDOW_MS;
    const list = (this.failures.get(key) ?? []).filter(time => time > limit);
    if (list.length) this.failures.set(key, list); else this.failures.delete(key);
    return list.length > HANDSHAKE_FAILURES_PER_WINDOW;
  }
  /** One check of the ikesu server at a time for the whole bridge: connections made again and again share it, so a
   *  device cannot start a check process per connection. Each connection still looks at its own state afterwards. */
  checkServer() {
    if (!this.serverCheck) this.serverCheck = Promise.resolve().then(() => this.serverAlive()).finally(() => { this.serverCheck = null; });
    return this.serverCheck;
  }
  onOpen(key, role, id) {
    if (this.tooManyFailures(id)) { this.transport.closeConn(key); return; }
    // The same id opened again: end the old connection first, so its screen and timers do not live on unseen
    // (a revoke or a reset only reaches the sessions in this map).
    this.sessions.get(key)?.close(false, false);
    // The relay allows 4 connections; a relay that sends more is not honoured past this many at once.
    if (this.sessions.size >= MAX_SESSIONS) { this.log('同時の接続が多すぎるため断りました'); this.transport.closeConn(key); return; }
    this.sessions.set(key, new Session(this, key, role, id));
  }
  onData(key, buffer) { void this.sessions.get(key)?.onData(buffer); }
  onClose(key) { this.sessions.get(key)?.close(false, false); }
  onReset() { for (const session of [...this.sessions.values()]) session.close(); }
  publish() { for (const session of this.sessions.values()) if (session.role === 'app' && session.ready) { void session.sendNotice(); if (session.operating) void session.sendSnapshot().catch(() => session.close()); } }
  clientCount() { let count = 0; for (const session of this.sessions.values()) if (session.role === 'app' && session.ready) count++; return count; }
  startPairing({ name, permissions }) {
    this.pairing?.close();
    this.pairing = new Pairing({ relay: this.relay, bridgePublic: this.noiseKey.publicKey, name, permissions, now: this.now });
    clearTimeout(this.pairingTimer);
    const pairing = this.pairing;
    this.pairingTimer = setTimeout(() => { if (this.pairing === pairing) this.closePairing(); }, Math.max(0, pairing.exp * 1000 - this.now()) + 50);
    this.pairingTimer.unref?.();
    return pairing;
  }
  appTag(device16) { return appTag(this.relay, device16); }
  // PC の端末で打たれた6桁を確かめ、合えば許可リストに入れる。
  async confirmPairing(code) {
    const pairing = this.pairing;
    const candidate = pairing?.confirm(code);
    if (!candidate) { if (pairing && !pairing.active) { this.closePairing(); } return null; }
    const device = { device: b64u(randomBytes(16)), noise_pub: b64u(candidate.noisePublic), operate_pub: candidate.operate, name: candidate.name,
      permissions: pairing.permissions, created: new Date(this.now()).toISOString() };
    await addDevice(this.configDir, device);
    await this.reloadDevices();
    const session = candidate.session;
    if (session.closed || !this.devices.some(current => current.device === device.device)) {
      throw Error('登録を反映できませんでした');
    }
    session.send({ type: 'paired', device: device.device, tag: this.appTag(Buffer.from(device.device, 'base64url')), name: pairing.name, permissions: pairing.permissions });
    session.paired = true;
    if (this.pairing === pairing) this.closePairing(session);
    const linger = setTimeout(() => session.close(), 10_000); linger.unref?.();
    return device;
  }
  closePairing(except) {
    const pairing = this.pairing; this.pairing = null; clearTimeout(this.pairingTimer);
    if (!pairing) return;
    for (const session of [...this.sessions.values()]) if (session.role === 'pair' && session !== except) session.close();
    pairing.close();
  }
  stop() { for (const session of [...this.sessions.values()]) session.close(); }
}
