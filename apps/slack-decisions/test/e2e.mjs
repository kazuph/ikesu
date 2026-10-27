// e2e: real ikesu server (isolated session) + real daemon + fake Slack.
//
//   node test/e2e.mjs [--out <dir>]
//
// Spawns `ikesu server` against throwaway XDG dirs, runs slack-decisions.mjs
// against it with Slack pointed at a local http+ws fake, creates a decision
// with `ikesu decision ask`, answers it by injecting a number reaction over
// the socket-mode websocket, and asserts the decision resolves with the
// picked option and the resolved Slack post is removed.

import { execFile, spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { FakeSlack, FakeWsServer, waitUntil as waitUntilBase } from "./fakes.mjs";

const waitUntil = (fn, options = {}) => waitUntilBase(fn, { timeout: 15_000, ...options });

const APP_ROOT = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const REPO_ROOT = path.resolve(APP_ROOT, "..", "..");
const IKESU = process.env.SLACK_DECISIONS_TEST_IKESU || process.env.IKESU_BIN_PATH || "ikesu";
const DAEMON = path.join(APP_ROOT, "slack-decisions.mjs");

const outIndex = process.argv.indexOf("--out");
const OUT =
  outIndex >= 0
    ? process.argv[outIndex + 1]
    : path.join(REPO_ROOT, ".artifacts", "feat-slack-decisions", "e2e");
fs.mkdirSync(OUT, { recursive: true });

const tmp = fs.mkdtempSync("/tmp/ikesu-slack-");
const configHome = path.join(tmp, "config");
const runtimeDir = path.join(tmp, "runtime");
const stateDir = path.join(tmp, "state");
const socketPath = path.join(tmp, "ikesu-state", "default", "ikesu.sock");
fs.mkdirSync(path.join(configHome, "ikesu"), { recursive: true });
fs.mkdirSync(runtimeDir, { recursive: true });
fs.mkdirSync(stateDir, { recursive: true });
fs.writeFileSync(path.join(configHome, "ikesu", "config.toml"), "[experimental]\nallow_nested = true\n");

const ikesuEnv = {
  XDG_CONFIG_HOME: configHome,
  IKESU_CONFIG: path.join(configHome, "ikesu", "config.toml"),
  IKESU_STATE_DIR: path.join(tmp, "ikesu-state"),
  XDG_RUNTIME_DIR: runtimeDir,
  IKESU_SOCKET_PATH: socketPath,
  IKESU_SOCKET_PATH_EXPLICIT: "1",
  SHELL: "/bin/sh",
};

const serverLog = fs.createWriteStream(path.join(OUT, "server.log"));
const daemonLog = fs.createWriteStream(path.join(OUT, "daemon.log"));

function spawnLogged(argv, env, out) {
  const child = spawn(argv[0], argv.slice(1), {
    env: { ...process.env, ...env },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.on("data", (c) => out.write(c));
  child.stderr.on("data", (c) => out.write(c));
  return child;
}

const cleanup = [];
function finish(code) {
  for (const fn of cleanup.reverse()) {
    try {
      fn();
    } catch {}
  }
  process.exit(code);
}

async function main() {
  const evidence = { steps: [] };
  const step = (name, ok, detail) => {
    evidence.steps.push({ name, ok, detail });
    process.stdout.write(`${ok ? "PASS" : "FAIL"} ${name}${detail ? ` — ${detail}` : ""}\n`);
  };

  // 1. isolated ikesu server
  const server = spawnLogged([IKESU, "server"], ikesuEnv, serverLog);
  cleanup.push(() => server.kill("SIGTERM"));
  await waitUntil(
    () =>
      new Promise((resolve) => {
        const sock = net.connect(socketPath);
        sock.once("connect", () => {
          sock.destroy();
          resolve(true);
        });
        sock.once("error", () => resolve(false));
      }),
  );
  step("isolated ikesu server up", true, socketPath);

  // 2. fake Slack (web api + socket mode endpoint)
  const fake = await new FakeSlack().start();
  const ws = await new FakeWsServer().start();
  fake.wsUrl = ws.url;
  cleanup.push(() => fake.stop());
  cleanup.push(() => ws.stop());

  // 3. the real daemon, glasses absent (no saberante socket) -> immediate post
  const daemonEnv = {
    ...ikesuEnv,
    IKESU_BIN_PATH: IKESU,
    SLACK_BOT_TOKEN: "xoxb-e2e",
    SLACK_CHANNEL: "C-e2e",
    SLACK_APP_TOKEN: "xapp-e2e",
    SLACK_DECISIONS_API_BASE: fake.base,
    SLACK_DECISIONS_STATE_DIR: stateDir,
    SLACK_DECISIONS_SABERA_SOCK: path.join(tmp, "no-sabera.sock"),
    SLACK_DECISIONS_NO_KEYCHAIN: "1",
    SLACK_DECISIONS_NO_PASS: "1",
    SLACK_DECISIONS_POLL_INTERVAL_MS: "200",
    HOME: tmp, // keep the real slack-ask env files out of this run
  };
  const daemon = spawnLogged(
    [process.execPath, DAEMON, "run"],
    daemonEnv,
    daemonLog,
  );
  cleanup.push(() => daemon.kill("SIGTERM"));
  await waitUntil(() => fake.socketUrls > 0);
  await ws.waitForClient();
  step("daemon socket-mode connection", true, `opens=${fake.socketUrls}`);

  // 4. create a real pending decision (blocks until answered)
  let askOut = "";
  let askCode = null;
  const ask = execFile(
    IKESU,
    [
      "decision",
      "ask",
      "--kind",
      "ask",
      "--title",
      "e2e: Slack から承認して",
      "--body",
      "feat/slack-decisions の動作確認",
      "--option",
      "approve:承認する:approve",
      "--option",
      "reject:却下する:reject",
      "--allow-text",
      "--timeout",
      "120000",
    ],
    { env: { ...process.env, ...ikesuEnv }, encoding: "utf8" },
    (error, stdout, stderr) => {
      askOut = `${stdout}\n${stderr}`;
      askCode = error?.code ?? 0;
    },
  );
  cleanup.push(() => ask.kill("SIGTERM"));

  // 5. fake slack receives the post
  await waitUntil(() => fake.posts.length > 0);
  const post = fake.posts[0];
  fs.writeFileSync(
    path.join(OUT, "post.json"),
    JSON.stringify(post, null, 2),
  );
  step("decision posted to fake slack", true, `ts=${post.ts}`);

  // 6. human reacts with :one: over the socket-mode websocket
  ws.send({
    envelope_id: "e2e-env-1",
    type: "events_api",
    payload: {
      type: "event_callback",
      event: {
        type: "reaction_added",
        user: "UHUMAN",
        reaction: "one",
        item: { type: "message", channel: "C-e2e", ts: post.ts },
      },
    },
  });

  // 7. decision ask resolves with the picked option
  await waitUntil(() => askCode !== null, { timeout: 15_000 });
  fs.writeFileSync(path.join(OUT, "ask-output.txt"), askOut);
  const saved = JSON.parse(askOut.trim()).result.decision;
  const answered = saved.answer.option_id === "approve" && saved.status === "answered";
  step("ikesu decision ask resolved", askCode === 0, askOut.trim().slice(0, 200));
  step("answer recorded as approve via slack", answered && askOut.includes('"slack"'), askOut.trim().slice(0, 200));

  // The migrated plugin removes resolved posts, preserving the source behavior.
  await waitUntil(() => fake.deletes.some((u) => u.ts === post.ts));
  step("resolved Slack post closed by deletion", true, post.ts);

  // Exercise the actual guard process and its feedback, using the real isolated
  // server. echo is harmless; the default policy asks for the word "deploy".
  const marker = path.join(tmp, "guard", "agent-panes", "proof.json");
  fs.mkdirSync(path.dirname(marker), { recursive: true });
  fs.writeFileSync(marker, JSON.stringify({
    pane_id: "proof", agent: "codex", launch_cwd: tmp,
    socket: socketPath, mode: "enforce", guard_config: {},
  }));
  for (const scenario of [
    { name: "guard-approve", reaction: "one", code: 0, expected: "deploy" },
    { name: "guard-reject", reaction: "three", code: 1, expected: "理由: 未入力" },
    { name: "guard-comment", text: "この試験コマンドは実行しないで", code: 1, expected: "理由: この試験コマンドは実行しないで" },
  ]) {
    let output = "", code = null;
    const priorTs = new Set(fake.posts.map(p => p.ts));
    const guard = execFile(IKESU, ["guard", "exec", "--", "/bin/echo", "deploy"], {
      cwd: tmp, encoding: "utf8", env: { ...process.env, ...ikesuEnv,
        HOME: tmp, IKESU_GUARD_MARKER: marker, IKESU_PANE_ID: "proof",
        AI_GUARD_REQUEST_REASON: "対象プロジェクトのSlack承認試験。無害なechoだけを実行するため。",
        AI_GUARD_TEMP_APPROVAL_FILE: path.join(tmp, scenario.name + "-approval"),
      },
    }, (err, stdout, stderr) => { output = stdout + stderr; code = err?.code ?? 0; });
    cleanup.push(() => guard.kill("SIGTERM"));
    await waitUntil(() => { if (code !== null) throw Error(scenario.name + " ended before Slack post: " + output); return fake.posts.some(p => !priorTs.has(p.ts)); });
    const guardPost = fake.posts.find(p => !priorTs.has(p.ts));
    ws.send({ envelope_id: scenario.name, type: "events_api", payload: {
      type: "event_callback", event: scenario.text
        ? { type: "message", user: "UHUMAN", thread_ts: guardPost.ts, ts: String(Number(guardPost.ts)+1), text: scenario.text }
        : { type: "reaction_added", user: "UHUMAN", reaction: scenario.reaction, item: { type: "message", channel: "C-e2e", ts: guardPost.ts } },
    }});
    await waitUntil(() => code !== null, { timeout: 15_000 });
    fs.writeFileSync(path.join(OUT, scenario.name + ".txt"), output);
    step(scenario.name + " feedback", code === scenario.code && output.includes(scenario.expected), output.trim());
    await waitUntil(() => fake.deletes.some(p => p.ts === guardPost.ts));
    step(scenario.name + " post closed", true, guardPost.ts);
  }

  const rpc = (method, params) => new Promise((resolve, reject) => {
    const socket = net.connect(socketPath);
    let buffer = "";
    socket.on("connect", () => socket.write(JSON.stringify({ id: "slack-proof", method, params }) + "\n"));
    socket.on("error", reject);
    socket.on("data", data => {
      buffer += data;
      if (!buffer.includes("\n")) return;
      socket.end();
      const response = JSON.parse(buffer.split("\n")[0]);
      response.error ? reject(Error(JSON.stringify(response.error))) : resolve(response.result);
    });
  });
  const question = { kind: "ask", title: "cancel and recreate proof", body: "same question",
    allow_text: true, options: [{ id: "yes", label: "Yes", role: "approve" }] };
  const firstCancel = (await rpc("decision.create", question)).decision;
  await waitUntil(() => fake.posts.some(p => JSON.stringify(p.blocks).includes(firstCancel.decision_id)));
  await rpc("decision.cancel", { decision_id: firstCancel.decision_id });
  await waitUntil(() => fake.posts.length === 0);
  step("cancelled decision removed from Slack", (await rpc("decision.get", { decision_id: firstCancel.decision_id })).decision.status === "cancelled", firstCancel.decision_id);
  const original = (await rpc("decision.create", question)).decision;
  await waitUntil(() => fake.posts.length === 1);
  await rpc("decision.cancel", { decision_id: original.decision_id });
  const replacement = (await rpc("decision.create", question)).decision;
  await waitUntil(() => fake.posts.length === 1 && JSON.stringify(fake.posts[0].blocks).includes(replacement.decision_id));
  // Let any cancellation delete that was in flight settle before checking again.
  await rpc("decision.get", { decision_id: replacement.decision_id });
  step("recreated question has exactly one pending Slack post", fake.posts.length === 1 && !JSON.stringify(fake.posts[0].blocks).includes("取り消し"), replacement.decision_id);
  await rpc("decision.cancel", { decision_id: replacement.decision_id });
  await waitUntil(() => fake.posts.length === 0);
  const expiring = (await rpc("decision.create", { ...question, timeout_ms: 200 })).decision;
  await waitUntil(() => fake.posts.length === 1);
  await waitUntil(() => fake.posts.length === 0);
  step("expired decision removed from Slack", (await rpc("decision.get", { decision_id: expiring.decision_id })).decision.status === "expired", expiring.decision_id);

  fs.writeFileSync(
    path.join(OUT, "slack-state.json"),
    JSON.stringify(
      {
        posts: fake.posts,
        updates: fake.updates,
        threadReplies: Object.fromEntries(fake.threadReplies),
        calls: fake.calls.map((c) => c.method),
      },
      null,
      2,
    ),
  );
  fs.writeFileSync(path.join(OUT, "summary.json"), JSON.stringify(evidence, null, 2));

  const failed = evidence.steps.filter((s) => !s.ok);
  process.stdout.write(
    `\n${failed.length === 0 ? "E2E OK" : "E2E FAILED"} — evidence in ${OUT}\n`,
  );
  finish(failed.length === 0 ? 0 : 1);
}

main().catch((error) => {
  process.stderr.write(`e2e failed: ${error?.stack || error}\n`);
  finish(1);
});
