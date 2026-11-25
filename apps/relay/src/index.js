// ikesu の Android アプリと各 PC の bridge をつなぐ中継（apps/android/PROTOCOL.md の 2 章）。
// 中継は暗号文を渡すだけで、Noise の鍵は持たない。ここでは接続タグの照合と部屋への振り分けだけをする。
import { Room } from "./room.js";

export { Room };

const enc = new TextEncoder();
const HEX64 = /^[0-9a-f]{64}$/;
const B64URL = /^[A-Za-z0-9_-]+$/;

function b64urlDecode(s) {
  if (!B64URL.test(s)) return null;
  const b = s.replace(/-/g, "+").replace(/_/g, "/");
  const pad = b + "=".repeat((4 - (b.length % 4)) % 4);
  try {
    const bin = atob(pad);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch {
    return null;
  }
}

function b64urlEncode(bytes) {
  return btoa(String.fromCharCode(...bytes)).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");
}

function canonical(segment, length) {
  const bytes = b64urlDecode(segment);
  return bytes?.length === length && b64urlEncode(bytes) === segment ? bytes : null;
}

function fromHex(h) {
  const out = new Uint8Array(h.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(h.slice(i * 2, i * 2 + 2), 16);
  return out;
}

function concat(...parts) {
  const n = parts.reduce((a, p) => a + p.length, 0);
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) {
    out.set(p, o);
    o += p.length;
  }
  return out;
}

let keyCache = { raw: null, key: null };
async function relayKey(raw) {
  if (keyCache.raw === raw && keyCache.key) return keyCache.key;
  const bytes = b64urlDecode(raw || "");
  if (!bytes || bytes.length !== 32) throw new Error("RELAY_KEY が base64url の32バイトではない");
  const key = await crypto.subtle.importKey("raw", bytes, { name: "HMAC", hash: "SHA-256" }, false, ["verify"]);
  keyCache = { raw, key };
  return key;
}

// crypto.subtle.verify は定数時間で比べる。
async function tagOk(env, header, message) {
  if (!header || !header.startsWith("IKESU-RELAY ")) return false;
  const hex = header.slice("IKESU-RELAY ".length);
  if (!HEX64.test(hex)) return false;
  const key = await relayKey(env.RELAY_KEY);
  return crypto.subtle.verify("HMAC", key, fromHex(hex), message);
}

function u64be(n) {
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, BigInt(n));
  return out;
}

const bad = (status, msg) => new Response(msg, { status });

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const seg = url.pathname.split("/").filter((s, i) => !(i === 0 && s === ""));
    // seg: ["v1", kind, ...]
    if (seg[0] !== "v1") return bad(404, "not found");
    const kind = seg[1];
    const want = { bridge: 1, app: 2, pair: 3 }[kind];
    if (want === undefined || seg.length !== 2 + want) return bad(404, "not found");
    if (request.method !== "GET") return bad(405, "method not allowed");

    const room = canonical(seg[2], 32);
    if (!room) return bad(400, "bad room");
    let label, rest, role, extra;
    if (kind === "bridge") {
      label = "bridge";
      rest = new Uint8Array(0);
      role = 0;
    } else if (kind === "app") {
      const dev = canonical(seg[3], 16);
      if (!dev) return bad(400, "bad device");
      label = "app";
      rest = dev;
      role = 1;
      extra = dev;
    } else {
      const pid = canonical(seg[3], 16);
      if (!pid) return bad(400, "bad pairing");
      if (!/^[0-9]{1,15}$/.test(seg[4])) return bad(400, "bad exp");
      const exp = Number(seg[4]);
      label = "pair";
      rest = concat(pid, u64be(exp));
      role = 2;
      extra = pid;
      if (exp < Math.floor(Date.now() / 1000)) return bad(401, "expired");
    }

    let ok = false;
    try {
      ok = await tagOk(env, request.headers.get("Authorization"), concat(enc.encode(label), room, rest));
    } catch (e) {
      return bad(500, "relay not configured");
    }
    if (!ok) return bad(401, "unauthorized");
    if (request.headers.get("Upgrade")?.toLowerCase() !== "websocket") return bad(426, "websocket required");

    const stub = env.ROOM.get(env.ROOM.idFromName(seg[2]));
    const h = new Headers(request.headers);
    h.set("x-ikesu-role", String(role));
    if (extra) h.set("x-ikesu-id", Array.from(extra, (b) => b.toString(16).padStart(2, "0")).join(""));
    return stub.fetch(new Request(request, { headers: h }));
  },
};
