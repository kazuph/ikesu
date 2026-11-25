import { DurableObject } from "cloudflare:workers";

const MAX_APPS = 4;
const MAX_MSG = 65535;
const MAX_MSGS_PER_SEC = 200;
const MAX_BYTES_PER_SEC = 4 * 1024 * 1024;

const OPEN = 0x01;
const DATA = 0x02;
const CLOSE = 0x03;
const ALLOW = 0x04;
const MAX_ALLOW = 4095;

const hex = (u) => Array.from(u, (b) => b.toString(16).padStart(2, "0")).join("");
const unhex = (h) => Uint8Array.from(h.match(/../g), (x) => parseInt(x, 16));

// 1部屋 = 1つの Durable Object。bridge 1本とアプリ最大4本の WebSocket を Hibernation API で持つ。
// 接続ごとの役割と ID は serializeAttachment に置く（Hibernation で消えない）。
export class Room extends DurableObject {
  // 毎秒の上限の数え方は、接続が起きている間だけ持つメモリ上の値。
  rate = new WeakMap();

  constructor(ctx, env) {
    super(ctx, env);
    ctx.blockConcurrencyWhile(async () => {
      const stored = await ctx.storage.get("allow");
      this.allow = Array.isArray(stored) ? new Set(stored) : undefined;
    });
  }

  // 今つながっている bridge（置き換え済み・閉じかけのものは除く）。
  bridge(except) {
    return (
      this.ctx
        .getWebSockets("bridge")
        .find((w) => w !== except && w.readyState === 1 && !w.deserializeAttachment()?.replaced) ?? null
    );
  }

  async fetch(request) {
    const role = Number(request.headers.get("x-ikesu-role"));
    const peer = request.headers.get("x-ikesu-id");
    if (role === 1 && this.allow && !this.allow.has(peer)) {
      return new Response("device not allowed", { status: 403 });
    }
    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    if (role === 0) {
      // 新しい bridge が古いものを置き換える。古い bridge の接続 ID は新しい bridge には通じないので、
      // 既存のアプリ接続も切って、つなぎ直させる。
      const olds = this.ctx.getWebSockets("bridge");
      for (const old of olds) {
        old.serializeAttachment({ role: 0, replaced: true });
        try { old.close(1000, "replaced"); } catch {}
      }
      if (olds.length) {
        for (const a of this.ctx.getWebSockets("app")) this.dropApp(a, 1012, "bridge replaced", false);
      }
      this.ctx.acceptWebSocket(server, ["bridge"]);
      server.serializeAttachment({ role: 0 });
      // bridge 不在の間に待っていたアプリ（初めての bridge のとき）へ open を出す。
      if (!olds.length) {
        for (const a of this.ctx.getWebSockets("app")) server.send(this.openFrame(a.deserializeAttachment()).buffer);
      }
      return new Response(null, { status: 101, webSocket: client });
    }

    if (this.ctx.getWebSockets("app").length >= MAX_APPS) {
      return new Response("too many connections", { status: 429 });
    }
    const id = crypto.getRandomValues(new Uint8Array(16));
    const idHex = hex(id);
    const att = {
      role,
      id: idHex,
      peer,
      at: Date.now(),
    };
    this.ctx.acceptWebSocket(server, ["app", "c:" + idHex]);
    server.serializeAttachment(att);

    const bridge = this.bridge();
    if (bridge) {
      bridge.send(this.openFrame(att).buffer);
    } else {
      await this.scheduleWait();
    }
    return new Response(null, { status: 101, webSocket: client });
  }

  openFrame(att) {
    const out = new Uint8Array(1 + 16 + 1 + 16);
    out[0] = OPEN;
    out.set(unhex(att.id), 1);
    out[17] = att.role; // 1 = app, 2 = pair
    out.set(unhex(att.peer), 18);
    return out;
  }

  waitMs() {
    const n = Number(this.env.APP_WAIT_MS);
    return Number.isFinite(n) && n > 0 ? n : 30000;
  }

  async scheduleWait() {
    const cur = await this.ctx.storage.getAlarm();
    if (cur === null) await this.ctx.storage.setAlarm(Date.now() + this.waitMs());
  }

  // bridge がいない間に待たされたアプリのうち、期限を過ぎたものを切る。
  async alarm() {
    if (this.bridge()) return;
    const now = Date.now();
    let next = null;
    for (const a of this.ctx.getWebSockets("app")) {
      const att = a.deserializeAttachment();
      const due = att.at + this.waitMs();
      if (due <= now) this.dropApp(a, 1013, "bridge unavailable", false);
      else next = next === null ? due : Math.min(next, due);
    }
    if (next !== null) await this.ctx.storage.setAlarm(next);
  }

  // アプリ接続を切る。notify が true なら bridge へ close を送る。
  dropApp(ws, code, reason, notify) {
    const att = ws.deserializeAttachment() || {};
    if (notify && !att.dropped) this.sendClose(att.id);
    ws.serializeAttachment({ ...att, dropped: true });
    try { ws.close(code, reason); } catch {}
  }

  sendClose(idHex) {
    const b = this.bridge();
    if (!b || !idHex) return;
    const out = new Uint8Array(17);
    out[0] = CLOSE;
    out.set(unhex(idHex), 1);
    try { b.send(out.buffer); } catch {}
  }

  overRate(ws, size) {
    const now = Date.now();
    let r = this.rate.get(ws);
    if (!r || now - r.start >= 1000) {
      r = { start: now, msgs: 0, bytes: 0 };
      this.rate.set(ws, r);
    }
    r.msgs++;
    r.bytes += size;
    return r.msgs > MAX_MSGS_PER_SEC || r.bytes > MAX_BYTES_PER_SEC;
  }

  async webSocketMessage(ws, message) {
    const att = ws.deserializeAttachment() || {};
    if (att.role === 0) return await this.fromBridge(ws, message);
    // アプリ: 素の Noise メッセージ1通。
    if (att.dropped) return;
    if (typeof message === "string") return this.dropApp(ws, 1003, "binary only", true);
    if (message.byteLength > MAX_MSG) return this.dropApp(ws, 1009, "message too big", true);
    if (this.overRate(ws, message.byteLength)) return this.dropApp(ws, 1008, "rate limit", true);
    const b = this.bridge();
    if (!b) return;
    const out = new Uint8Array(17 + message.byteLength);
    out[0] = DATA;
    out.set(unhex(att.id), 1);
    out.set(new Uint8Array(message), 17);
    b.send(out.buffer);
  }

  async fromBridge(ws, message) {
    if (this.bridge() !== ws) return;
    if (typeof message === "string" || message.byteLength < 17) {
      return ws.close(1002, "bad frame");
    }
    const frame = new Uint8Array(message);
    const type = frame[0];
    if (type === ALLOW) return await this.setAllow(ws, frame);
    if (type !== DATA && !(type === CLOSE && frame.byteLength === 17)) return;
    const [app] = this.ctx.getWebSockets("c:" + hex(frame.subarray(1, 17)));
    if (!app) return;
    if (type === DATA) {
      const body = frame.subarray(17);
      if (body.length > MAX_MSG) return this.dropApp(app, 1009, "message too big", true);
      app.send(body.slice().buffer);
    } else {
      this.dropApp(app, 1000, "closed by bridge", false);
    }
  }

  async setAllow(ws, frame) {
    const body = frame.subarray(17);
    if (frame.subarray(1, 17).some(byte => byte !== 0) || body.length % 16 !== 0 || body.length / 16 > MAX_ALLOW) {
      return ws.close(1002, "bad frame");
    }
    const allow = new Set();
    for (let i = 0; i < body.length; i += 16) allow.add(hex(body.subarray(i, i + 16)));
    if (this.allow?.size === allow.size && [...allow].every(peer => this.allow.has(peer))) return;
    try {
      await this.ctx.storage.put("allow", [...allow]);
    } catch {
      return ws.close(1011, "allow store failed");
    }
    this.allow = allow;
    for (const app of this.ctx.getWebSockets("app")) {
      const att = app.deserializeAttachment();
      if (att?.role === 1 && !att.dropped && !allow.has(att.peer)) {
        this.dropApp(app, 1008, "device revoked", true);
      }
    }
  }

  async webSocketClose(ws, code, reason) {
    this.closed(ws);
  }

  async webSocketError(ws) {
    this.closed(ws);
    try { ws.close(1011, "error"); } catch {}
  }

  closed(ws) {
    const att = ws.deserializeAttachment() || {};
    if (att.role === 0) {
      // 置き換えられた古い bridge の close は、今の bridge とアプリに影響させない。
      if (att.replaced || this.bridge(ws)) return;
      for (const a of this.ctx.getWebSockets("app")) this.dropApp(a, 1001, "bridge gone", false);
    } else if (!att.dropped) {
      this.sendClose(att.id);
      ws.serializeAttachment({ ...att, dropped: true });
    }
  }
}
