import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { once } from "node:events";
import { MacDialogDecisionsDaemon } from "../daemon.mjs";

// Local authorization service emulator. Requests traverse a real socket;
// authorization and first-answer-wins are service state, never canned calls.
async function answerService(t) {
  const state = {authorized: false, resolved: false, requests: [], answers: []};
  const server = http.createServer(async (request, response) => {
    let body = "";
    for await (const chunk of request) body += chunk;
    const args = JSON.parse(body);
    state.requests.push(args);
    if (!state.authorized) { response.writeHead(403); response.end("authorize at https://login.tailscale.com/a/test-service"); return; }
    if (state.resolved) { response.writeHead(409); response.end("decision_already_resolved"); return; }
    state.resolved = true;
    state.answers.push(args);
    response.end("answered");
  });
  server.listen(0, "127.0.0.1"); await once(server, "listening");
  t.after(() => new Promise(resolve => server.close(resolve)));
  const runAnswer = async args => {
    const response = await fetch(`http://127.0.0.1:${server.address().port}`, {method: "POST", body: JSON.stringify(args)});
    return {code: response.ok ? 0 : 1, stderr: await response.text()};
  };
  return {state, runAnswer};
}

function pending(d, id = "dec-test") {
  const entry = {key: `mini ${id}`, machineId: "mini", machine: {id: "mini"}, decision: {decision_id: id, title: "Question", options: []}, resolved: false, queued: false, dismissed: false, showing: false, machineDown: false, answerPending: false};
  d.entries.set(entry.key, entry);
  return entry;
}

async function until(predicate) {
  const deadline = Date.now() + 2_000;
  while (!predicate()) {
    if (Date.now() > deadline) throw Error("service condition did not settle");
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

test("切断中に回答と再承認待ちを保持し、再通知で二重送信しない", async t => {
  const {state, runAnswer} = await answerService(t);
  const d = new MacDialogDecisionsDaemon({config: {answerRetryMs: 5}, runAnswer, log: () => {}});
  t.after(() => { d.stopped = true; for (const e of d.entries.values()) clearTimeout(e.answerRetryTimer); });
  const entry = pending(d);
  const answer = {optionIds: ["yes"], text: "free text retained"};
  assert.equal(await d.submitAnswer(entry, answer), false);
  d.onMachineDown("mini", "disconnected");
  await new Promise(resolve => setTimeout(resolve, 25));
  assert.equal(state.requests.length, 1);
  assert.equal(d.entries.get(entry.key), entry);
  assert.equal(entry.retry.text, answer.text);
  assert.deepEqual(entry.retry.optionIds, answer.optionIds);
  d.onWatchLine({event: "machine_up", machine: {id: "mini"}});
  for (let i = 0; i < 3; i++) d.onCreated("mini", {id: "mini"}, entry.decision);
  assert.equal(d.queue.length, 0);
  state.authorized = true;
  await until(() => state.answers.length === 1 && d.entries.size === 0);
  assert.equal(state.requests.length, 2);
  assert.deepEqual(state.answers[0].slice(-2), ["--text", answer.text]);
  assert.equal(entry.answerPending, false);
  assert.equal(entry.retry, null);
  d.onCreated("mini", {id: "mini"}, entry.decision);
  d.onResolved("mini", {...entry.decision, status: "answered"});
  assert.equal(d.entries.size, 0);
  assert.equal(d.queue.length, 0);
});

test("他所の回答が確定すると再送を止め、既決応答でも状態を回収する", async t => {
  const {state, runAnswer} = await answerService(t);
  const d = new MacDialogDecisionsDaemon({config: {answerRetryMs: 5}, runAnswer, log: () => {}});
  t.after(() => { d.stopped = true; for (const e of d.entries.values()) clearTimeout(e.answerRetryTimer); });
  const entry = pending(d);
  await d.submitAnswer(entry, {optionIds: ["yes"], text: ""});
  d.onResolved("mini", {...entry.decision, status: "answered"});
  await new Promise(resolve => setTimeout(resolve, 25));
  assert.equal(state.requests.length, 1);
  assert.equal(state.answers.length, 0);
  state.authorized = true;
  state.resolved = true;
  const already = pending(d, "dec-already");
  assert.equal(await d.submitAnswer(already, {optionIds: ["yes"]}), true);
  assert.equal(d.entries.size, 0);
  assert.equal(already.answerPending, false);
});

test("送信中に切断しても成功応答を回収し、未確定の別回答は残す", async t => {
  const {state, runAnswer} = await answerService(t);
  state.authorized = true;
  const d = new MacDialogDecisionsDaemon({config: {}, runAnswer, log: () => {}});
  const entry = pending(d), other = pending(d, "dec-other");
  const sent = d.submitAnswer(entry, {optionIds: ["yes"], text: "in flight"});
  assert.equal(entry.answerPending, true);
  d.onMachineDown("mini", "disconnected");
  assert.equal(await sent, true);
  assert.equal(d.entries.has(entry.key), false);
  assert.equal(d.entries.get(other.key), other);
  assert.equal(state.answers.length, 1);
});
