// [notify] の振り分け: 偽の時計と偽の Slack API だけで確かめる。外の宛先には送らない。
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import assert from "node:assert/strict";
import { SlackClient, SlackDecisionsDaemon, notifyPlan } from "../slack-decisions.mjs";
import { FakeSlack, waitUntil } from "./fakes.mjs";
import { TestClock } from "./clock.mjs";

const LOCAL = { id: "local", label: "Local" };
const MIN = 60_000;
const CFG = { enabled: true, reach_timeout_ms: 30_000, warnings: [], channels: [
  { id: "mac-panel", send: "now", after_ms: 0, fallback_for: [] },
  { id: "android", send: "now", after_ms: 0, fallback_for: [] },
  { id: "slack", send: "unanswered", after_ms: 5 * MIN, fallback_for: ["android"] },
] };

function decision(clock, overrides = {}) {
  return { decision_id: "dec-n1", kind: "ask", title: "進めてよい？", body: "本文", options: [{ id: "yes", label: "はい", role: "approve" }],
    allow_text: false, status: "pending", created_unix_ms: clock.now(), expires_unix_ms: clock.now() + 3600_000, origin: { pane_id: "p1" }, ...overrides };
}

async function setup({ notifyConfig = async () => CFG, glasses = null, config = {}, clock = new TestClock() } = {}) {
  const fake = await new FakeSlack().start();
  const logs = [];
  const daemon = new SlackDecisionsDaemon({
    clock, config: { postDelayMs: 80, pollIntervalMs: 40, socketMode: false, saberaSocket: "/tmp/definitely-not-there.sock",
      stateDir: fs.mkdtempSync(path.join(os.tmpdir(), "slack-notify-")), ...config },
    slack: new SlackClient({ token: "t", channel: "C1", apiBase: fake.base }), botUserId: "UBOT",
    spawnWatch: async () => { throw new Error("tests drive onWatchLine directly"); },
    runAnswer: async () => ({ code: 0, stdout: "{}", stderr: "" }), getDecision: async () => null,
    glassesState: async () => glasses, notifyConfig, log: message => logs.push(message),
  });
  await daemon.reloadNotify();
  const settle = () => new Promise(resolve => setTimeout(resolve, 150));
  return { fake, daemon, clock, logs, settle, done: async () => { await daemon.stop(); await fake.stop(); } };
}
const created = (daemon, d) => daemon.onWatchLine({ event: "created", machine: LOCAL, decision: d });
const updated = (daemon, d) => daemon.onWatchLine({ event: "updated", machine: LOCAL, decision: d });

test("notifyPlan: 3つの決まりと、無効のときの null", () => {
  const now = 1_000_000;
  assert.equal(notifyPlan(null, {}, now), null);
  assert.equal(notifyPlan({ ...CFG, enabled: false }, {}, now), null);
  assert.equal(notifyPlan({ ...CFG, channels: CFG.channels.map(c => c.id === "slack" ? { ...c, send: "now" } : c) }, {}, now), null);
  assert.equal(notifyPlan({ ...CFG, channels: CFG.channels.slice(0, 2) }, {}, now), null);
  assert.equal(notifyPlan(CFG, { created_unix_ms: 5, reach: { android: { state: "no_client", at_unix_ms: 6 } } }, now).at, now);
  assert.equal(notifyPlan(CFG, { created_unix_ms: 5, reach: { android: { state: "shown", at_unix_ms: 100 } } }, now).at, 100 + 5 * MIN);
  assert.equal(notifyPlan(CFG, { created_unix_ms: 5 }, now).at, 5 + 30_000);
  // fallback_for に無い送り先の reach は見ない
  assert.equal(notifyPlan(CFG, { created_unix_ms: 5, reach: { "mac-panel": { state: "no_client", at_unix_ms: 6 } } }, now).at, 5 + 30_000);
});

test("no_client が記録されていれば、待たずにすぐ投稿する", async () => {
  const t = await setup();
  try {
    created(t.daemon, decision(t.clock, { reach: { android: { state: "no_client", at_unix_ms: t.clock.now() } } }));
    await waitUntil(() => t.fake.posts.length === 1);
    assert.ok(t.logs.some(line => line.includes("no_client")), t.logs.join("\n"));
  } finally { await t.done(); }
});

test("shown から5分たっても答えが無ければ投稿し、それまでは投稿しない", async () => {
  const t = await setup();
  try {
    created(t.daemon, decision(t.clock));
    await t.settle();
    // 届いた返事が来る
    await t.clock.advance(10_000);
    updated(t.daemon, decision(t.clock, { created_unix_ms: t.clock.now() - 10_000, reach: { android: { state: "shown", at_unix_ms: t.clock.now() } } }));
    // いま created から10秒。shown は10秒時点なので、投稿は10秒＋5分＝310秒時点。
    await t.clock.advance(299_000);
    await t.settle();
    assert.equal(t.fake.posts.length, 0, "shown から4分59秒ではまだ投稿しない");
    await t.clock.advance(1_000);
    await waitUntil(() => t.fake.posts.length === 1);
    assert.ok(t.logs.some(line => line.includes("shown+300秒")), t.logs.join("\n"));
  } finally { await t.done(); }
});

test("shown のあと5分以内に答えが出たら投稿しない", async () => {
  const t = await setup();
  try {
    const shown = { android: { state: "shown", at_unix_ms: t.clock.now() } };
    created(t.daemon, decision(t.clock, { reach: shown }));
    await t.settle();
    await t.clock.advance(2 * MIN);
    t.daemon.onWatchLine({ event: "resolved", machine: LOCAL, decision: decision(t.clock, { reach: shown, status: "answered" }) });
    await t.settle();
    await t.clock.advance(10 * MIN);
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
  } finally { await t.done(); }
});

test("reach が無いまま reach_timeout（30秒）を過ぎたらすぐ投稿する", async () => {
  const t = await setup();
  try {
    created(t.daemon, decision(t.clock));
    await t.settle();
    await t.clock.advance(29_000);
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
    await t.clock.advance(1_000);
    await waitUntil(() => t.fake.posts.length === 1);
    assert.ok(t.logs.some(line => line.includes("届かない")), t.logs.join("\n"));
  } finally { await t.done(); }
});

test("待っている間に no_client が来たら、残り時間を待たずに投稿する", async () => {
  const t = await setup();
  try {
    const d = decision(t.clock);
    created(t.daemon, d);
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
    updated(t.daemon, { ...d, reach: { android: { state: "no_client", at_unix_ms: t.clock.now() } } });
    await waitUntil(() => t.fake.posts.length === 1);
  } finally { await t.done(); }
});

test("再起動しても、判断待ちの時刻と reach から残りの時間で投稿する", async () => {
  const clock = new TestClock();
  const shownAt = clock.now() - 3 * MIN;
  const d = decision(clock, { created_unix_ms: shownAt - 5_000, reach: { android: { state: "shown", at_unix_ms: shownAt } } });
  const t = await setup({ clock });
  try {
    created(t.daemon, d); // watch の最初の一覧（created の再送）で届く
    await t.settle();
    await clock.advance(2 * MIN - 1_000);
    await t.settle();
    assert.equal(t.fake.posts.length, 0, "残り2分より前は投稿しない");
    await clock.advance(1_000);
    await waitUntil(() => t.fake.posts.length === 1);
  } finally { await t.done(); }
});

test("[notify] が無い・無効のときは今の動き（メガネ未接続ならすぐ、接続中なら postDelayMs 後）", async () => {
  for (const notifyConfig of [null, async () => ({ enabled: false, reach_timeout_ms: 30000, channels: [], warnings: [] })]) {
    const t = await setup({ notifyConfig, glasses: null });
    try {
      created(t.daemon, decision(t.clock));
      await waitUntil(() => t.fake.posts.length === 1);
    } finally { await t.done(); }
    const g = await setup({ notifyConfig, glasses: "connected", config: { postDelayMs: 120 } });
    try {
      created(g.daemon, decision(g.clock));
      await g.settle().then(() => {});
      await waitUntil(() => g.fake.posts.length === 1);
    } finally { await g.done(); }
  }
});

test("設定の読み込みに失敗したら今の動きに戻り、ログに理由を残す", async () => {
  const t = await setup({ notifyConfig: async () => { throw new Error("config notify が失敗"); } });
  try {
    assert.ok(t.logs.some(line => line.includes("今の動きに戻します") && line.includes("config notify が失敗")), t.logs.join("\n"));
    created(t.daemon, decision(t.clock));
    await waitUntil(() => t.fake.posts.length === 1);
  } finally { await t.done(); }
});

test("設定が読み直されて有効になると、待機中の判断待ちを計算し直す", async () => {
  let current = null;
  const t = await setup({ notifyConfig: async () => { if (!current) throw new Error("まだ無い"); return current; }, glasses: "connected", config: { postDelayMs: 60_000 } });
  try {
    created(t.daemon, decision(t.clock, { reach: { android: { state: "no_client", at_unix_ms: t.clock.now() } } }));
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
    current = CFG;
    await t.daemon.reloadNotify();
    await waitUntil(() => t.fake.posts.length === 1);
  } finally { await t.done(); }
});

test("切断中のマシンの判断待ちは、振り分けの時刻が来ても投稿しない", async () => {
  const t = await setup();
  try {
    t.daemon.onWatchLine({ event: "machine_down", machine: LOCAL, reason: "ssh" });
    await t.settle();
    created(t.daemon, decision(t.clock, { reach: { android: { state: "no_client", at_unix_ms: t.clock.now() } } }));
    await t.settle();
    await t.clock.advance(10 * MIN);
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
  } finally { await t.done(); }
});

test("待機中に設定が消える・無効になる・読み込みが失敗すると、今の動きに戻って投稿される", async () => {
  for (const next of [async () => ({ enabled: false, reach_timeout_ms: 30000, channels: [], warnings: [] }),
    async () => ({ ...CFG, channels: CFG.channels.filter(c => c.id !== "slack") }),
    async () => { throw new Error("config notify が失敗"); }]) {
    let current = async () => CFG;
    const t = await setup({ notifyConfig: () => current(), glasses: null });
    try {
      created(t.daemon, decision(t.clock));
      await t.settle();
      assert.equal(t.fake.posts.length, 0, "待機中");
      current = next;
      await t.daemon.reloadNotify();
      await waitUntil(() => t.fake.posts.length === 1);
    } finally { await t.done(); }
  }
});

test("待機中に設定が消えてメガネ接続中なら、postDelayMs 後に投稿する（今の動き）", async () => {
  let current = async () => CFG;
  const t = await setup({ notifyConfig: () => current(), glasses: "connected", config: { postDelayMs: 150 } });
  try {
    created(t.daemon, decision(t.clock));
    await t.settle();
    assert.equal(t.fake.posts.length, 0);
    current = async () => { throw new Error("読めない"); };
    await t.daemon.reloadNotify();
    await new Promise(resolve => setTimeout(resolve, 60));
    assert.equal(t.fake.posts.length, 0, "postDelayMs の前");
    await waitUntil(() => t.fake.posts.length === 1);
  } finally { await t.done(); }
});

test("メガネの待ち（今の動き）の最中に有効な設定を読むと、メガネの待ちで先に投稿されない", async () => {
  let current = async () => { throw new Error("まだ無い"); };
  const u = await setup({ notifyConfig: () => current(), glasses: "connected", config: { postDelayMs: 200 } });
  try {
    current = async () => { throw new Error("まだ無い"); };
    await u.daemon.reloadNotify();
    created(u.daemon, decision(u.clock));
    await new Promise(resolve => setTimeout(resolve, 30));
    current = async () => CFG;
    await u.daemon.reloadNotify();
    await new Promise(resolve => setTimeout(resolve, 350));
    assert.equal(u.fake.posts.length, 0, "メガネの待ち（200ms）で投稿されない");
    await u.clock.advance(30_000);
    await waitUntil(() => u.fake.posts.length === 1);
  } finally { await u.done(); }
});

test("待ち時間が setTimeout の上限を超えても、上限で切って待つ", async () => {
  const t = await setup({ notifyConfig: async () => ({ ...CFG, reach_timeout_ms: 5_000_000_000 }) });
  try {
    created(t.daemon, decision(t.clock));
    await t.settle();
    const entry = [...t.daemon.entries.values()][0];
    assert.ok(entry.routeTimer);
    assert.equal(entry.routeTimer.at - t.clock.now(), 2 ** 31 - 1);
    assert.equal(t.fake.posts.length, 0);
  } finally { await t.done(); }
});
