import {TestClock} from './clock.mjs';
// Fake-Slack tests for the slack-decisions daemon. No real Slack requests are
// made here: SlackClient.apiBase is pointed at a local http server, Socket
// Mode is pointed at a local ws server, and the Sabera check is either a stub
// function or a fake unix-socket ws server.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFile } from "node:child_process";
import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import {
  saberaGlassesState,
  SlackClient,
  SlackDecisionsDaemon,
  REACTION_GRACE_MS,
} from "../slack-decisions.mjs";
import {
  FakeSlack,
  FakeWsServer,
  startFakeSabera,
  waitUntil,
} from "./fakes.mjs";

const PLUGIN_ROOT = path.dirname(fileURLToPath(import.meta.url));
const APP_ROOT = path.dirname(PLUGIN_ROOT);
const DAEMON_PATH = path.join(APP_ROOT, "slack-decisions.mjs");


// A Slack Socket event describes a mutation already stored by Slack. Keep
// the local API emulator coherent so expiry can verify the actual history.
async function deliver(daemon, api, payload) {
 const event=payload?.type==='event_callback'?payload.event:payload;
 if(event?.type==='reaction_added')api.userReact(event.item.ts,event.reaction,event.user);
 if(event?.type==='reaction_removed') {
  api.reactionsByTs.set(event.item.ts,(api.reactionsByTs.get(event.item.ts)||[]).map(reaction=>reaction.name===event.reaction?{...reaction,users:reaction.users.filter(user=>user!==event.user)}:reaction));
 }
 if(event?.type==='message'&&event.thread_ts&&event.ts) {
  const rows=api.threadReplies.get(event.thread_ts)||[];
  if(!rows.some(row=>row.ts===event.ts))rows.push(event);
  api.threadReplies.set(event.thread_ts,rows);
 }
 return daemon.handleSocketPayload(payload);
}

function tmpdir(name) {
  return fs.mkdtempSync(path.join(os.tmpdir(), `slack-decisions-${name}-`));
}

function sampleDecision(overrides = {}) {
  return {
    decision_id: "dec-test-1",
    kind: "pane.propose_kill",
    title: "pane p1136 を終了してよい？",
    body: "git status で完了確認済み",
    options: [
      { id: "opt-approve", label: "承認" },
      { id: "opt-reject", label: "却下" },
    ],
    allow_text: false,
    status: "pending",
    created_unix_ms: Date.now(),
    expires_unix_ms: Date.now() + 3600_000,
    origin: {
      pane_id: "p1136",
      agent: "claude",
      cwd: "/tmp/repo",
      command: "just test",
      reason: "no output for 600s",
    },
    ...overrides,
  };
}

function makeDaemon({
  slack,
  config: configOverrides = {},
  glasses = null,
  runAnswer = async () => ({ code: 0, stdout: "{}", stderr: "" }),
  getDecision = async () => null,
  postsFilePath = null,
} = {}) {
  const answers = [];
  const clock=new TestClock();
  const daemon = new SlackDecisionsDaemon({
    clock,
    config: {
      postDelayMs: 80,
      pollIntervalMs: 40,
      socketMode: false,
      saberaSocket: "/tmp/definitely-not-there.sock",
      stateDir: tmpdir("state"),
      ...configOverrides,
    },
    slack,
    botUserId: "UBOT",
    spawnWatch: async () => {
      throw new Error("tests drive onWatchLine directly");
    },
    runAnswer: async (argv) => {
      answers.push(argv);
      return runAnswer(argv);
    },
    getDecision,
    glassesState: async () => glasses,
    log: () => {},
    postsFilePath,
  });
  daemon.testAnswers = answers;
  daemon.testClock=clock;
  return daemon;
}

function feed(daemon, event) {
  daemon.onWatchLine(event);
}

async function feedAndSettle(daemon, event, ms = 300) {
  daemon.onWatchLine(event);
  await new Promise((r) => setTimeout(r, ms));
}

const LOCAL = { id: "local", label: "Local" };

// --- tests -------------------------------------------------------------------

test("created + glasses absent -> posts immediately with full context", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({
    token: "t",
    channel: "C1",
    apiBase: fake.base,
  });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    assert.equal(fake.posts.length, 1);
    const post = fake.posts[0];
    assert.equal(post.channel, "C1");
    const text = JSON.stringify(post.blocks);
    assert.match(text, /pane p1136 を終了してよい/);
    assert.match(text, /ローカル/);
    assert.match(text, /p1136/); // pane
    assert.match(text, /claude/); // agent
    assert.match(text, /\/tmp\/repo/); // cwd
    assert.match(text, /just test/); // command
    assert.match(text, /no output for 600s/); // reason
    assert.match(text, /:one: 承認/);
    assert.match(text, /:two: 却下/);
    assert.match(text, /期限/);
    // number reactions were added for each option
    await waitUntil(()=>fake.reactionAddCalls.length===2);
    const names = fake.reactionAddCalls.map((c) => c.name);
    assert.deepEqual(names, ["one", "two"]);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("glasses connected -> waits the configured delay, posts if still pending", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: "connected", config: { postDelayMs: 120 } });
  try {
    feed(daemon, { event: "created", machine: LOCAL, decision: sampleDecision() });
    await new Promise((r) => setTimeout(r, 50));
    assert.equal(fake.posts.length, 0, "must not post before the delay");
    await waitUntil(() => fake.posts.length > 0);
    assert.equal(fake.posts.length, 1);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("glasses connected -> resolved during delay is never posted", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: "connected", config: { postDelayMs: 100 } });
  try {
    feed(daemon, { event: "created", machine: LOCAL, decision: sampleDecision() });
    await new Promise((r) => setTimeout(r, 30));
    feed(daemon, {
      event: "resolved",
      machine: LOCAL,
      decision: { ...sampleDecision(), status: "answered" },
    });
    await new Promise((r) => setTimeout(r, 200));
    assert.equal(fake.posts.length, 0);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("duplicated created (watch reconnect) posts only once", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    const event = { event: "created", machine: LOCAL, decision: sampleDecision() };
    feed(daemon, event);
    feed(daemon, event);
    feed(daemon, event);
    await new Promise((r) => setTimeout(r, 80));
    assert.equal(fake.posts.length, 1);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("number reaction -> ikesu decision answer <id> <option> --machine --responder slack", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: { id: "darwin-mini", label: "darwin mini" },
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "reaction_added",
        user: "UHUMAN",
        reaction: "two",
        item: { type: "message", channel: "C1", ts },
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    assert.deepEqual(daemon.testAnswers[0], [
      "decision",
      "answer",
      "dec-test-1",
      "opt-reject",
      "--responder",
      "slack",
      "--machine",
      "darwin-mini",
    ]);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("thread reply '2' answers with option 2", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "message",
        user: "UHUMAN",
        text: "２",
        ts: "1000.9001",
        thread_ts: ts,
        channel: "C1",
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    assert.equal(daemon.testAnswers[0][3], "opt-reject");
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("allow_text thread reply forwards --text", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({
        decision_id: "dec-text",
        allow_text: true,
        options: [{ id: "opt-ok", label: "OK" }],
      }),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "message",
        user: "UHUMAN",
        text: "条件付きで承認、先に dry-run",
        ts: "1000.9002",
        thread_ts: ts,
        channel: "C1",
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    const argv = daemon.testAnswers[0];
    assert.ok(!argv.includes(""), "text-only answer must omit the option argument");
    assert.deepEqual(argv.slice(3), [
      "--responder",
      "slack",
      "--machine",
      "local",
      "--text",
      "条件付きで承認、先に dry-run",
    ]);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("free text on a non-allow_text decision is refused in thread", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "message",
        user: "UHUMAN",
        text: "たぶんOK",
        ts: "1000.9003",
        thread_ts: ts,
        channel: "C1",
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 0);
    const notes = (fake.threadReplies.get(ts) || []).filter((m) => m.bot_id);
    assert.equal(notes.length, 1);
    assert.match(notes[0].text, /自由記述/);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("resolved answered -> post and bot thread replies are deleted", async () => {
  // A resolved decision's post must not linger: Slack keeps only posts
  // that still need an answer. The bot's own thread replies are deleted
  // with it; a human's reply cannot be deleted with a bot token (it
  // leaves the channel view together with the parent post).
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    const botNote = await slack.postThreadReply(ts, "ボットの返信");
    const humanNote = fake.userReply(ts, "店長の返信");
    await feedAndSettle(daemon, {
      event: "resolved",
      machine: LOCAL,
      decision: {
        ...sampleDecision(),
        status: "answered",
        answer: {
          option_id: "opt-approve",
          responder: "tui",
          answered_unix_ms: Date.now(),
        },
      },
    });
    assert.equal(fake.posts.length, 0, "the resolved post was deleted");
    const deletedTs = fake.deletes.map((d) => d.ts);
    assert.ok(deletedTs.includes(ts), "chat.delete called for the post");
    assert.ok(deletedTs.includes(botNote.ts), "bot reply deleted too");
    assert.ok(
      !deletedTs.includes(humanNote),
      "a human reply is not deleted by the bot",
    );
    assert.equal(daemon.entries.size, 0);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("answered via a Slack reply -> resolved event deletes the post", async () => {
  // Answering on Slack itself still ends with the post deleted once the
  // resolved event arrives — Slack keeps only unanswered posts.
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "message",
        subtype: undefined,
        user: "UHUMAN",
        text: "2",
        channel: "C1",
        ts: "1000.9002",
        thread_ts: ts,
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    await waitUntil(() => daemon.testAnswers.length === 1);
    await feedAndSettle(daemon, {
      event: "resolved",
      machine: LOCAL,
      decision: {
        ...sampleDecision(),
        status: "answered",
        answer: {
          option_id: "opt-reject",
          responder: "slack",
          answered_unix_ms: Date.now(),
        },
      },
    });
    assert.equal(fake.posts.length, 0);
    assert.ok(fake.deletes.some((d) => d.ts === ts));
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("resolved expired and cancelled delete posts without waiting for reuse expiry", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack });
  try {
    for (const status of ["expired", "cancelled"]) {
      const decision = sampleDecision({ decision_id: "resolved-" + status });
      await daemon.onCreated("local", LOCAL, decision);
      const ts = fake.posts[0].ts;
      await daemon.onResolved("local", { ...decision, status });
      assert.equal(fake.posts.length, 0);
      assert.ok(fake.deletes.some(d => d.ts === ts));
      assert.equal(fake.updates.length, 0);
    }
  } finally { await daemon.stop(); await fake.stop(); }
});

test("recreated same question right after cancel reuses the post", async () => {
  // Screen-prompt churn rebuilds the pending decision under a new id;
  // the Slack post must not stack — the cancelled decision's post is
  // adopted and updated back to pending.
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    const question = {
      kind: "agent_prompt",
      title: "複数質問の確認（2つ目）。「いぬ」を選んでください",
      question_label: "質問 2/2 動物",
      options: [
        { id: "1", label: "ねこ" },
        { id: "2", label: "いぬ" },
      ],
      allow_text: true,
      origin: { pane_id: "p2301", agent: "claude", reason: "screen_prompt" },
    };
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ ...question, decision_id: "dec-a" }),
    });
    assert.equal(fake.posts.length, 1);
    const ts = fake.posts[0].ts;

    // Churn: cancelled, then recreated under a new id with the same
    // question payload.
    const cancellation = daemon.onResolved("local", {
      ...sampleDecision({ ...question, decision_id: "dec-a" }), status: "cancelled",
    });
    const creation = daemon.onCreated("local", LOCAL,
      sampleDecision({ ...question, decision_id: "dec-b" }));
    await Promise.all([cancellation, creation]);
    assert.equal(fake.posts[0].ts, ts, "existing post adopted before deletion starts");
    assert.equal(fake.posts.length, 1, "no second post for the same question");
    const backToPending = fake.updates.filter((u) => u.ts === ts);
    assert.ok(
      backToPending.some((u) =>
        JSON.stringify(u.blocks).match(/質問 2\/2 動物/),
      ),
      "the reused post renders the recreated decision as pending",
    );

    // A different question still gets its own post.
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({
        ...question,
        decision_id: "dec-c",
        title: "別の質問",
      }),
    });
    assert.equal(fake.posts.length, 2, "a different question posts anew");
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("machine_down deletes posts; a still-pending decision is re-posted on machine_up", async () => {
  // A down machine's questions cannot be answered, so their posts are
  // deleted; when the machine returns and the decision is still pending,
  // it is posted again.
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: "connected", config: { postDelayMs: 80 } });
  try {
    feed(daemon, {
      event: "machine_down",
      machine: { id: "darwin-mini" },
      reason: "ssh dead",
    });
    feed(daemon, {
      event: "created",
      machine: { id: "darwin-mini", label: "darwin mini" },
      decision: sampleDecision({ decision_id: "dec-down" }),
    });
    await new Promise((r) => setTimeout(r, 160));
    assert.equal(fake.posts.length, 0, "down machine's pending is not posted");

    // posted decision of a machine that then goes down
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ decision_id: "dec-up" }),
    }, 300);
    // local machine is not down: it posts via delay timer
    await waitUntil(() => fake.posts.length === 1);
    const ts = fake.posts[0].ts;
    feed(daemon, { event: "machine_down", machine: LOCAL, reason: "lost" });
    await waitUntil(() => fake.posts.length === 0);
    assert.ok(
      fake.deletes.some((d) => d.ts === ts),
      "the down machine's post is deleted",
    );

    // machine_up: the created replay re-posts the still-pending decision
    await feedAndSettle(daemon, {
      event: "machine_up",
      machine: LOCAL,
    });
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ decision_id: "dec-up" }),
    });
    await waitUntil(() => fake.posts.length === 1);
    assert.notEqual(
      fake.posts[0].ts,
      ts,
      "a fresh post replaces the deleted one",
    );
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("restart reconcile deletes restored posts whose decisions already resolved", async () => {
  // Posts left over from before a restart are checked on machine_up;
  // ones whose decisions are no longer pending are deleted so Slack
  // keeps only unanswered posts.
  const dir = tmpdir("reconcile");
  const postsFile = path.join(dir, "posts.json");
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const first = makeDaemon({ slack, glasses: null, postsFilePath: postsFile });
  try {
    await feedAndSettle(first, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ decision_id: "dec-gone" }),
    });
    assert.equal(fake.posts.length, 1);
    const ts = fake.posts[0].ts;
    await first.stop();

    const second = makeDaemon({
      slack,
      glasses: null,
      postsFilePath: postsFile,
      getDecision: async () => ({
        ...sampleDecision({ decision_id: "dec-gone" }),
        status: "answered",
      }),
    });
    try {
      second.loadPostsFile();
      assert.equal(second.posts.size, 1, "posts file restored");
      feed(second, { event: "machine_up", machine: LOCAL });
      await waitUntil(() => fake.posts.length === 0, { timeout: 6_000 });
      assert.ok(
        fake.deletes.some((d) => d.ts === ts),
        "the resolved decision's leftover post is deleted",
      );
    } finally {
      await second.stop();
    }
  } finally {
    await fake.stop();
  }
});

test("decision_already_resolved -> thread note, no retry loop", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({
    slack,
    glasses: null,
    runAnswer: async () => ({
      code: 1,
      stdout: "",
      stderr: "error: decision_already_resolved",
    }),
  });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "reaction_added",
        user: "UHUMAN",
        reaction: "one",
        item: { type: "message", ts },
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    const notes = (fake.threadReplies.get(ts) || []).filter((m) => m.bot_id);
    assert.equal(notes.length, 1);
    assert.match(notes[0].text, /決定済み/);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("answer blocked by Tailscale re-approval -> link in thread, resent until it lands", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  let calls = 0;
  const daemon = makeDaemon({
    slack,
    glasses: null,
    config: { answerRetryMs: 60 },
    runAnswer: async () => {
      calls += 1;
      if (calls < 3) {
        return {
          code: 255,
          stdout: "",
          stderr: "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/abc123",
        };
      }
      return { code: 0, stdout: "{}", stderr: "" };
    },
  });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: { id: "m0e51", label: "仕事Mac" },
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: { type: "reaction_added", user: "UHUMAN", reaction: "one", item: { type: "message", ts } },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    await new Promise((r) => setTimeout(r, 400));
    assert.equal(calls, 3, "resent until the machine took it");
    const sent = daemon.testAnswers;
    assert.ok(sent.every((argv) => argv.includes("m0e51") && argv.includes("--machine")));
    const notes = (fake.threadReplies.get(ts) || []).filter((m) => m.bot_id);
    assert.equal(notes.length, 1, "one note per approval link");
    assert.match(notes[0].text, /仕事Mac/);
    assert.match(notes[0].text, /https:\/\/login\.tailscale\.com\/a\/abc123/);
    assert.match(notes[0].text, /自動で送り直します/);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("history poll recovers a reaction that Socket Mode missed", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    fake.userReact(ts, "one");
    await daemon.pollOnce();
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    assert.equal(daemon.testAnswers[0][3], "opt-approve");
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("history poll recovers a thread reply that Socket Mode missed", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ allow_text: true, decision_id: "dec-poll" }),
    });
    const ts = fake.posts[0].ts;
    fake.userReply(ts, "ポーリング経由の回答");
    await daemon.pollOnce();
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    const argv = daemon.testAnswers[0];
    assert.equal(argv.at(-2), "--text");
    assert.equal(argv.at(-1), "ポーリング経由の回答");
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("socket mode end-to-end: envelope over real ws drives an answer", async () => {
  const fake = await new FakeSlack().start();
  const ws = await new FakeWsServer().start();
  fake.wsUrl = ws.url;
  const slack = new SlackClient({
    token: "t",
    appToken: "xapp-fake",
    channel: "C1",
    apiBase: fake.base,
  });
  const daemon = makeDaemon({ slack, glasses: null, config: { socketMode: true } });
  try {
    daemon.socketLoop().catch(() => {});
    await ws.waitForClient();
    assert.equal(fake.socketUrls, 1, "exactly one socket-mode connection");
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    const ts = fake.posts[0].ts;
    fake.userReact(ts,"one");
    ws.send({
      envelope_id: "env-1",
      type: "events_api",
      payload: {
        type: "event_callback",
        event: {
          type: "reaction_added",
          user: "UHUMAN",
          reaction: "one",
          item: { type: "message", channel: "C1", ts },
        },
      },
    });
    await waitUntil(() => [...daemon.entries.values()].some(entry=>entry.reactionDraft));
    await daemon.testClock.advance(REACTION_GRACE_MS);
    await waitUntil(() => daemon.testAnswers.length > 0);
    assert.equal(daemon.testAnswers[0][3], "opt-approve");
    await waitUntil(() =>
      ws.received.some((m) => JSON.parse(m).envelope_id === "env-1"),
    );
  } finally {
    await daemon.stop();
    await ws.stop();
    await fake.stop();
  }
});

test("posts file restore -> restart resumes answering without reposting", async () => {
  const dir = tmpdir("restore");
  const postsFile = path.join(dir, "posts.json");
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const first = makeDaemon({ slack, glasses: null, postsFilePath: postsFile });
  try {
    await feedAndSettle(first, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    assert.equal(fake.posts.length, 1);
    await first.stop();

    const second = makeDaemon({ slack, glasses: null, postsFilePath: postsFile });
    second.loadPostsFile();
    await feedAndSettle(second, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision(),
    });
    assert.equal(fake.posts.length, 1, "no duplicate post after restart");
    const ts = fake.posts[0].ts;
    await deliver(second,fake,{
      type: "event_callback",
      event: {
        type: "reaction_added",
        user: "UHUMAN",
        reaction: "two",
        item: { type: "message", ts },
      },
    });
    await second.testClock.advance(REACTION_GRACE_MS);
    assert.equal(second.testAnswers.length, 1);
    await second.stop();
  } finally {
    await fake.stop();
  }
});

test("saberaGlassesState reads the unix-socket ws handshake", async () => {
  const dir = tmpdir("sabera");
  const sockPath = path.join(dir, "relay.sock");
  const fake = await startFakeSabera({ glasses: "connected", sockPath });
  try {
    assert.equal(await saberaGlassesState(sockPath), "connected");
  } finally {
    fake.stop();
  }
  const gone = await saberaGlassesState(path.join(dir, "gone.sock"), 300);
  assert.equal(gone, null);
});

test("non-kazuph user -> exits without touching Slack", async () => {
  const state = tmpdir("notme");
  const result = await new Promise((resolve) => {
    execFile(
      process.execPath,
      ["--input-type=module", "-e", `import {main} from ${JSON.stringify(new URL("../slack-decisions.mjs", import.meta.url).href)}; await main({argv:["node","daemon","run"], userInfo:()=>({username:"someoneelse"})});`],
      {
        env: {
          ...process.env,
          SLACK_DECISIONS_OS_USER: "someoneelse",
          SLACK_DECISIONS_STATE_DIR: state,
          SLACK_BOT_TOKEN: "xoxb-fake",
          SLACK_CHANNEL: "C1",
          SLACK_APP_TOKEN: "",
          SLACK_DECISIONS_API_BASE: "http://127.0.0.1:1",
        },
        timeout: 10_000,
      },
      (error, stdout, stderr) =>
        resolve({ code: error?.code ?? 0, stdout, stderr }),
    );
  });
  assert.equal(result.code, 0);
  assert.match(result.stderr, /Slack 送信をしません/);
});

test("kazuph but no credentials -> exits without posting", async () => {
  const state = tmpdir("nocreds");
  const result = await new Promise((resolve) => {
    execFile(
      process.execPath,
      [DAEMON_PATH, "run"],
      {
        env: {
          ...process.env,
          SLACK_DECISIONS_OS_USER: "kazuph",
          SLACK_DECISIONS_STATE_DIR: state,
          SLACK_BOT_TOKEN: "",
          SLACK_CHANNEL: "",
          SLACK_CHANNEL_ID: "",
          SLACK_APP_TOKEN: "",
          SLACK_DECISIONS_CONFIG: path.join(state, "missing.json"),
          SLACK_DECISIONS_NO_KEYCHAIN: "1",
          SLACK_DECISIONS_NO_PASS: "1",
          HOME: state,
        },
        timeout: 10_000,
      },
      (error, stdout, stderr) =>
        resolve({ code: error?.code ?? 0, stdout, stderr }),
    );
  });
  assert.equal(result.code, 0);
  assert.match(result.stderr, /資格情報/);
});

test("multi_select reactions accumulate then white_check_mark submits option_ids", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({
        multi_select: true,
        options: [
          { id: "opt-a", label: "検索" },
          { id: "opt-b", label: "追加" },
          { id: "opt-c", label: "通知" },
        ],
      }),
    });
    const ts = fake.posts[0].ts;
    const react = async (reaction) =>
      deliver(daemon,fake,{
        type: "event_callback",
        event: {
          type: "reaction_added",
          user: "UHUMAN",
          reaction,
          item: { type: "message", channel: "C1", ts },
        },
      });
    await react("one");
    await react("three");
    // Number reactions only stage the selection; nothing is answered yet.
    assert.equal(daemon.testAnswers.length, 0);
    await react("white_check_mark");
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    assert.deepEqual(daemon.testAnswers[0], [
      "decision",
      "answer",
      "dec-test-1",
      "opt-a",
      "opt-c",
      "--responder",
      "slack",
      "--machine",
      "local",
    ]);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("multi_select white_check_mark without picks posts a note instead of answering", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({ multi_select: true }),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "reaction_added",
        user: "UHUMAN",
        reaction: "white_check_mark",
        item: { type: "message", channel: "C1", ts },
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 0);
    const notes = [...fake.threadReplies.values()]
      .flat()
      .map((reply) => reply.text)
      .join("\n");
    assert.match(notes, /番号リアクション/);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("multi_select thread reply '1,3' submits option_ids", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, glasses: null });
  try {
    await feedAndSettle(daemon, {
      event: "created",
      machine: LOCAL,
      decision: sampleDecision({
        multi_select: true,
        options: [
          { id: "opt-a", label: "検索" },
          { id: "opt-b", label: "追加" },
          { id: "opt-c", label: "通知" },
        ],
      }),
    });
    const ts = fake.posts[0].ts;
    await deliver(daemon,fake,{
      type: "event_callback",
      event: {
        type: "message",
        user: "UHUMAN",
        text: "1,3",
        ts: "1000.9001",
        thread_ts: ts,
        channel: "C1",
      },
    });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 1);
    assert.deepEqual(daemon.testAnswers[0].slice(3, 5), ["opt-a", "opt-c"]);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

for (const status of ["answered", "cancelled"]) {
  test(`resolution during post reconciles late ${status} post`, async () => {
    const fake = await new FakeSlack().start();
    const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
    const daemon = makeDaemon({ slack, config: { recreatePostReuseMs: 40 } });
    const post = slack.postMessage.bind(slack);
    let release;
    const blocked = new Promise((resolve) => { release = resolve; });
    let started;
    const posting = new Promise((resolve) => { started = resolve; });
    slack.postMessage = async (payload) => {
      started();
      await blocked;
      return post(payload);
    };
    const decision = sampleDecision();
    try {
      const created = daemon.onCreated("local", LOCAL, decision);
      await posting;
      await daemon.onResolved("local", { ...decision, status });
      release();
      await created;
      assert.equal(fake.posts.length, 0);
      assert.equal(daemon.posts.size, 0);
    } finally {
      release();
      await daemon.stop();
      await fake.stop();
    }
  });
}

test("communication failures retry and exhausted deletes survive restart cleanup", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const postsFilePath = path.join(tmpdir("retry-posts"), "posts.json");
  const config = { postMutationRetryMs: 2, postMutationAttempts: 3 };
  const daemon = makeDaemon({ slack, config, postsFilePath });
  const decision = sampleDecision();
  let attempts = 0;
  const remove = slack.deleteMessage.bind(slack);
  try {
    await daemon.onCreated("local", LOCAL, decision);
    slack.deleteMessage = async () => { attempts += 1; throw new TypeError("fetch failed"); };
    await daemon.onResolved("local", { ...decision, status: "answered" });
    assert.equal(attempts, 3);
    assert.equal(fake.posts.length, 1);
    assert.equal(daemon.posts.size, 1);
    assert.equal(Object.keys(JSON.parse(fs.readFileSync(postsFilePath))).length, 1);
    slack.deleteMessage = remove;
    const restarted = makeDaemon({ slack, config, postsFilePath,
      getDecision: async () => ({ ...decision, status: "answered" }) });
    restarted.loadPostsFile();
    await restarted.reconcileMachine("local");
    assert.equal(fake.posts.length, 0);
    assert.equal(restarted.posts.size, 0);
    await restarted.stop();
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("cancelled deletion recovers after transient communication errors", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, config: {
    recreatePostReuseMs: 40, postMutationRetryMs: 2, postMutationAttempts: 3,
  } });
  const update = slack.updateMessage.bind(slack);
  const remove = slack.deleteMessage.bind(slack);
  let updates = 0;
  let deletes = 0;
  slack.updateMessage = async (...args) => {
    if (++updates < 3) throw new TypeError("fetch failed");
    return update(...args);
  };
  slack.deleteMessage = async (...args) => {
    if (++deletes < 3) throw new TypeError("fetch failed");
    return remove(...args);
  };
  try {
    const decision = sampleDecision();
    await daemon.onCreated("local", LOCAL, decision);
    await daemon.onResolved("local", { ...decision, status: "cancelled" });
    assert.equal(updates, 0, "cancelled status is not rendered");
    await waitUntil(() => fake.posts.length === 0);
    assert.equal(deletes, 3);
    assert.equal(daemon.posts.size, 0);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("exhausted cancelled cleanup keeps posts for the next decision reconciliation", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const postsFilePath = path.join(tmpdir("cancel-retry"), "posts.json");
  const daemon = makeDaemon({ slack, postsFilePath, config: {
    recreatePostReuseMs: 25, postMutationRetryMs: 2, postMutationAttempts: 3,
  } });
  const remove = slack.deleteMessage.bind(slack);
  let attempts = 0;
  slack.deleteMessage = async () => { ++attempts; throw new TypeError("fetch failed"); };
  try {
    const decision = sampleDecision();
    await daemon.onCreated("local", LOCAL, decision);
    await daemon.onResolved("local", { ...decision, status: "cancelled" });
    await waitUntil(() => attempts === 3);
    assert.equal(daemon.posts.size, 1);
    assert.equal(fake.posts.length, 1);
    assert.equal(Object.keys(JSON.parse(fs.readFileSync(postsFilePath))).length, 1);
    slack.deleteMessage = remove;
    await daemon.onResolved("local", { ...decision, status: "answered" });
    assert.equal(daemon.posts.size, 0);
    assert.equal(fake.posts.length, 0);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("late reuse read cannot resolve or delete a post adopted by the next decision", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack });
  let release;
  const blocked = new Promise((resolve) => { release = resolve; });
  let started;
  const reading = new Promise((resolve) => { started = resolve; });
  const read = slack.message.bind(slack);
  const first = sampleDecision({ decision_id: "reuse-first" });
  const second = sampleDecision({ decision_id: "reuse-second" });
  const third = sampleDecision({ decision_id: "reuse-third" });
  try {
    await daemon.onCreated("local", LOCAL, first);
    const ts = fake.posts[0].ts;
    let held = false;
    slack.message = async (...args) => {
      if (!held) { held = true; started(); await blocked; }
      return read(...args);
    };
    const firstCancel = daemon.onResolved("local", { ...first, status: "cancelled" });
    const secondPost = daemon.onCreated("local", LOCAL, second);
    await reading;
    await firstCancel;
    const secondCancel = daemon.onResolved("local", { ...second, status: "cancelled" });
    const thirdPost = daemon.onCreated("local", LOCAL, third);
    await Promise.all([secondCancel, thirdPost]);
    const updateCount = fake.updates.length;
    release();
    await secondPost;
    await new Promise((resolve) => setTimeout(resolve, 100));
    assert.equal(fake.posts.length, 1);
    assert.equal(fake.posts[0].ts, ts);
    assert.equal(fake.updates.length, updateCount, "old completion must not update adopted post");
    assert.equal(fake.deletes.length, 0);
    assert.equal(daemon.reusablePosts.size, 0);
    assert.equal([...daemon.posts.values()][0].decisionId, third.decision_id);
    assert.doesNotMatch(JSON.stringify(fake.posts[0].blocks), /取り消し/);
  } finally {
    release();
    await daemon.stop();
    await fake.stop();
  }
});

test("machine down retries each live post only three times and still cleans a distinct orphan", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const postsFilePath = path.join(tmpdir("down-retry"), "posts.json");
  const daemon = makeDaemon({ slack, postsFilePath, config: {
    postMutationRetryMs: 2, postMutationAttempts: 3,
  } });
  const remove = slack.deleteMessage.bind(slack);
  let attempts = 0;
  try {
    await daemon.onCreated("local", LOCAL, sampleDecision({ decision_id: "live-down" }));
    const liveTs = fake.posts[0].ts;
    await daemon.onCreated("local", LOCAL, sampleDecision({ decision_id: "orphan-down" }));
    const orphanTs = fake.posts[1].ts;
    const orphanKey = [...daemon.entries].find(([, e]) => e.post.ts === orphanTs)[0];
    daemon.entries.delete(orphanKey);
    slack.deleteMessage = async (ts) => {
      if (ts === liveTs) { ++attempts; throw new TypeError("fetch failed"); }
      return remove(ts);
    };
    await daemon.onMachineDown("local", "test disconnect");
    assert.equal(attempts, 3);
    assert.equal(fake.posts.length, 1);
    assert.equal(fake.posts[0].ts, liveTs);
    assert.equal(daemon.posts.size, 1);
    assert.ok(fake.deletes.some((p) => p.ts === orphanTs));
    assert.equal(Object.keys(JSON.parse(fs.readFileSync(postsFilePath))).length, 1);
  } finally {
    await daemon.stop();
    await fake.stop();
  }
});

test("failed cancellation deletion settles before replacement adopts the surviving post", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack, config: { postMutationRetryMs: 2, postMutationAttempts: 3 } });
  let attempts = 0, started;
  const failing = new Promise(r => { started = r; });
  const first = sampleDecision({ decision_id: "retry-owner-old" });
  const next = sampleDecision({ decision_id: "retry-owner-new" });
  try {
    await daemon.onCreated("local", LOCAL, first);
    const ts = fake.posts[0].ts;
    slack.deleteMessage = async () => { ++attempts; started(); throw new TypeError("fetch failed"); };
    const cancellation = daemon.onResolved("local", { ...first, status: "cancelled" });
    await failing;
    const creation = daemon.onCreated("local", LOCAL, next);
    await Promise.all([cancellation, creation]);
    assert.equal(attempts, 3);
    assert.equal(fake.posts.length, 1);
    assert.equal(fake.posts[0].ts, ts);
    assert.doesNotMatch(JSON.stringify(fake.posts[0].blocks), /取り消し/);
    assert.equal([...daemon.posts.values()][0].decisionId, next.decision_id);
  } finally { await daemon.stop(); await fake.stop(); }
});

test("cancelled closes Slack immediately without waiting for the reuse window", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack });
  try {
    const decision = sampleDecision();
    await daemon.onCreated("local", LOCAL, decision);
    await daemon.onResolved("local", { ...decision, status: "cancelled" });
    assert.equal(fake.posts.length, 0);
    assert.equal(fake.updates.length, 0, "do not leave a cancelled status message");
  } finally { await daemon.stop(); await fake.stop(); }
});

test("recreation during in-flight cancellation deletion leaves one pending post", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack });
  let release;
  const held = new Promise(r => { release = r; });
  let entered;
  const deleting = new Promise(r => { entered = r; });
  const remove = slack.deleteMessage.bind(slack);
  slack.deleteMessage = async (...args) => { entered(); await held; return remove(...args); };
  try {
    const first = sampleDecision({ decision_id: "cancel-in-flight" });
    await daemon.onCreated("local", LOCAL, first);
    const cancelled = daemon.onResolved("local", { ...first, status: "cancelled" });
    await deleting;
    const recreated = daemon.onCreated("local", LOCAL, { ...first, decision_id: "recreated-in-flight" });
    release();
    await Promise.all([cancelled, recreated]);
    assert.equal(fake.posts.length, 1);
    assert.match(JSON.stringify(fake.posts[0].blocks), /recreated-in-flight/);
    assert.doesNotMatch(JSON.stringify(fake.posts[0].blocks), /取り消し/);
  } finally { release(); await daemon.stop(); await fake.stop(); }
});

test("reused post rejects delayed socket replies and reactions from the old generation", async () => {
  for (const type of ["message", "reaction_added"]) {
    const fake = await new FakeSlack().start();
    const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
    const daemon = makeDaemon({ slack });
    try {
      const created = Date.now();
      const first = sampleDecision({ decision_id: "old", allow_text: true });
      await daemon.onCreated("local", LOCAL, first);
      const ts = fake.posts[0].ts;
      await Promise.all([
        daemon.onResolved("local", { ...first, status: "cancelled" }),
        daemon.onCreated("local", LOCAL, { ...first, decision_id: "new", created_unix_ms: created }),
      ]);
      const event = type === "message"
        ? { type, user: "UHUMAN", thread_ts: ts, text: "old answer", ts: String(created / 1000 - 1) }
        : { type, user: "UHUMAN", item: { type: "message", ts }, reaction: "one", event_ts: String(created / 1000 - 1) };
      await deliver(daemon,fake,event);
      await daemon.testClock.advance(REACTION_GRACE_MS);
      assert.equal(daemon.testAnswers.length, 0, "old generation must never answer the new ID");
      if (type === "message") event.ts = String(created / 1000 + 1);
      else event.event_ts = String(created / 1000 + 1);
      await deliver(daemon,fake,event);
      await daemon.testClock.advance(REACTION_GRACE_MS);
      await deliver(daemon,fake,event);
      await daemon.testClock.advance(REACTION_GRACE_MS);
      assert.equal(daemon.testAnswers.length, 1, "a new generation answer is applied once");
      assert.ok(daemon.testAnswers[0].includes("new"));
      assert.equal(fake.posts.length, 1);
    } finally { await daemon.stop(); await fake.stop(); }
  }
});

test("reused post remains unanswerable while history baseline APIs fail", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const daemon = makeDaemon({ slack });
  try {
    const first = sampleDecision({ decision_id: "baseline-old" });
    await daemon.onCreated("local", LOCAL, first);
    const ts = fake.posts[0].ts;
    const handle = fake.handle.bind(fake);
    fake.handle = (method, params) => ["conversations.history", "conversations.replies"].includes(method)
      ? { ok: false, error: "ratelimited" } : handle(method, params);
    await Promise.all([
      daemon.onResolved("local", { ...first, status: "cancelled" }),
      daemon.onCreated("local", LOCAL, { ...first, decision_id: "baseline-new" }),
    ]);
    await deliver(daemon,fake,{ type: "reaction_added", user: "UHUMAN", reaction: "one", event_ts: String(Date.now()/1000+1), item: { type: "message", ts } });
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 0);
    fake.reactionsByTs.set(ts, [{ name: "one", users: ["UHUMAN"] }]);
    fake.handle = handle;
    await daemon.pollOnce();
    await daemon.testClock.advance(REACTION_GRACE_MS);
    assert.equal(daemon.testAnswers.length, 0, "baseline recovery seeds existing reactions before enabling answers");
    assert.equal(fake.posts.length, 1);
  } finally { await daemon.stop(); await fake.stop(); }
});

test("body and option descriptions distinguish questions during post reuse", async () => {
  for (const change of [{ body: "different reason" }, { options: [{ id: "opt-approve", label: "承認", description: "different consequence" }, { id: "opt-reject", label: "却下" }] }]) {
    const fake = await new FakeSlack().start();
    const daemon = makeDaemon({ slack: new SlackClient({ token: "t", channel: "C1", apiBase: fake.base }) });
    try {
      const first = sampleDecision({ decision_id: "identity-old" });
      await daemon.onCreated("local", LOCAL, first);
      await Promise.all([
        daemon.onResolved("local", { ...first, status: "cancelled" }),
        daemon.onCreated("local", LOCAL, { ...first, ...change, decision_id: "identity-new" }),
      ]);
      assert.equal(fake.posts.length, 1);
      assert.equal(fake.deletes.length, 1);
      assert.equal(fake.calls.filter(c => c.method === "chat.postMessage").length, 2, "different content must get its own post");
    } finally { await daemon.stop(); await fake.stop(); }
  }
});

test("reused post history and restored state reject old replies but recover a fresh reply", async () => {
  const fake = await new FakeSlack().start();
  const slack = new SlackClient({ token: "t", channel: "C1", apiBase: fake.base });
  const postsFilePath = path.join(tmpdir("generation-restore"), "posts.json");
  const daemon = makeDaemon({ slack, postsFilePath });
  let restored;
  try {
    const first = sampleDecision({ decision_id: "history-old", allow_text: true });
    await daemon.onCreated("local", LOCAL, first);
    const ts = fake.posts[0].ts;
    fake.reactionsByTs.set(ts, [{ name: "one", users: ["UOLD"] }]);
    const next = { ...first, decision_id: "history-new", created_unix_ms: Date.now() };
    await Promise.all([
      daemon.onResolved("local", { ...first, status: "cancelled" }),
      daemon.onCreated("local", LOCAL, next),
    ]);
    await daemon.stop();
    restored = makeDaemon({ slack, postsFilePath });
    restored.loadPostsFile();
    await restored.onCreated("local", LOCAL, next);
    const oldTs = String(next.created_unix_ms / 1000 - 1);
    fake.threadReplies.set(ts, [{ ts: oldTs, user: "UOLD", text: "old reply" }]);
    await restored.pollOnce();
    await restored.testClock.advance(REACTION_GRACE_MS);
    assert.equal(restored.testAnswers.length, 0);
    const freshTs = String(Date.now() / 1000 + 1);
    fake.threadReplies.get(ts).push({ ts: freshTs, user: "UFRESH", text: "fresh reply" });
    await restored.pollOnce();
    await restored.testClock.advance(REACTION_GRACE_MS);
    await restored.pollOnce();
    await restored.testClock.advance(REACTION_GRACE_MS);
    assert.equal(restored.testAnswers.length, 1);
    assert.ok(restored.testAnswers[0].includes("fresh reply"));
    assert.equal(fake.posts.length, 1);
  } finally { await daemon.stop(); if (restored) await restored.stop(); await fake.stop(); }
});


test('local Slack history prioritizes a later comment over numbered reactions; reaction-only mapping stays intact', async t => {
 const service=await new FakeSlack().start();t.after(()=>service.stop());
 const slack=new SlackClient({token:'local-emulator',channel:'CLOCAL',apiBase:service.base});
 const daemon=makeDaemon({slack});t.after(()=>daemon.stop());
 for(const socket of [false,true]) {
  const decision=sampleDecision({decision_id:'comment-'+socket,allow_text:true});
  await daemon.onCreated('local',{id:'local'},decision);
  const entry=[...daemon.entries.values()].find(e=>e.decision.decision_id===decision.decision_id);
  service.userReact(entry.post.ts,'one');const ts=service.userReply(entry.post.ts,'待って、削除しないで');
  if(socket) {await deliver(daemon,service,{type:'reaction_added',user:'UHUMAN',reaction:'one',event_ts:String(Number(ts)-0.00001),item:{type:'message',ts:entry.post.ts}});}
  else await daemon.pollOnce();
    await daemon.testClock.advance(REACTION_GRACE_MS);
  await daemon.testClock.advance(REACTION_GRACE_MS);
  const answer=daemon.testAnswers.at(-1);
  assert.ok(answer.includes('--text'));assert.equal(answer[answer.indexOf('--text')+1],'待って、削除しないで');assert.ok(!answer.includes('opt-approve'));
  entry.resolved=true;
 }
 const d=sampleDecision({decision_id:'reaction-only'});await daemon.onCreated('local',{id:'local'},d);
 const e=[...daemon.entries.values()].find(e=>e.decision.decision_id===d.decision_id);service.userReact(e.post.ts,'two');await daemon.pollOnce();
    await daemon.testClock.advance(REACTION_GRACE_MS);
 assert.ok(daemon.testAnswers.at(-1).includes('opt-reject'));assert.ok(!daemon.testAnswers.at(-1).includes('--text'));
});


test('a newer socket reaction keeps its mapped option when the only fresh reply predates it',async t=>{
 const service=await new FakeSlack().start();t.after(()=>service.stop());
 const daemon=makeDaemon({slack:new SlackClient({token:'local-emulator',channel:'CLOCAL',apiBase:service.base})});t.after(()=>daemon.stop());
 await daemon.onCreated('local',{id:'local'},sampleDecision({allow_text:true}));
 const entry=[...daemon.entries.values()][0],ts=service.userReply(entry.post.ts,'以前のコメント');
 await deliver(daemon,service,{type:'reaction_added',user:'UHUMAN',reaction:'two',event_ts:String(Number(ts)+0.00001),item:{type:'message',ts:entry.post.ts}});
 await daemon.testClock.advance(REACTION_GRACE_MS);
 assert.ok(daemon.testAnswers.at(-1).includes('opt-reject'));assert.ok(!daemon.testAnswers.at(-1).includes('--text'));
});
