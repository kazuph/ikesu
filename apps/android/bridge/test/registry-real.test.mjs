import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import net from 'node:net';
import { spawn, execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { DecisionRegistry } from '../registry.mjs';
import { listen, MAX_MESSAGE_BYTES } from '../socket.mjs';
import { WebSocket } from 'ws';
import { once } from 'node:events';
import { spawnDecisionWatch } from '../../../mac-dialog-decisions/mac-dialog-decisions.mjs';
import { ClientViews } from '../../../mac-dialog-decisions/client-views.mjs';

const exec = promisify(execFile);
const binary = process.env.IKESU_TEST_BIN || path.join(os.homedir(), '.local/bin/ikesu');
const session = 'android-decisions-test';
async function until(predicate) {
  const deadline = Date.now() + 10000;
  while (!await predicate()) {
    if (Date.now() >= deadline) throw Error('隔離サーバーの起動がタイムアウトしました');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}
test('隔離 ikesu の保存済み回答と送信番号の重複を照合する', async t => {
  const home = fs.mkdtempSync('/tmp/ad-test-');
  const config = path.join(home, 'config.toml');
  fs.writeFileSync(config, '[experimental]\nallow_nested=true\n');
  const env = { ...process.env, HOME: home, IKESU_CONFIG: config, IKESU_STATE_DIR: path.join(home, 'state'), IKESU_SOCKET_PATH: path.join(home, 'state', session, 'ikesu.sock'), IKESU_SOCKET_PATH_EXPLICIT: '1', SHELL: '/bin/sh' };
  const server = spawn(binary, ['--session', session, 'server'], { env, stdio: ['ignore', 'ignore', 'pipe'] });
  let stderr = ''; server.stderr.on('data', data => stderr += data);
  const exited = new Promise(resolve => server.once('close', resolve));
  const rpc = (method, params = {}) => new Promise((resolve, reject) => {
    const socket = net.createConnection(env.IKESU_SOCKET_PATH); let data = '';
    socket.on('error', reject);
    socket.on('connect', () => socket.write(JSON.stringify({ id: 'android-test', method, params }) + '\n'));
    socket.on('data', chunk => { data += chunk; if (!data.includes('\n')) return; socket.end(); const row = JSON.parse(data.split('\n')[0]); row.error ? reject(Error(JSON.stringify(row.error))) : resolve(row.result); });
  });
  t.after(async () => { try { await rpc('server.stop'); } catch {} await exited; fs.rmSync(home, { recursive: true, force: true }); });
  await until(() => { if (server.exitCode !== null) throw Error(stderr); return fs.existsSync(env.IKESU_SOCKET_PATH); });
  let sent = 0;
  const runAnswer = async args => {
    sent++;
    try { const result = await exec(binary, ['--session', session, ...args], { env }); return { code: 0, ...result }; }
    catch (error) { return { code: error.code, stdout: error.stdout, stderr: error.stderr }; }
  };
  const getDecision = async (machine, id) => JSON.parse((await exec(binary, ['--session', session, 'decision', 'get', id, '--machine', machine], { env })).stdout).result.decision;
  const views = new ClientViews(env);
  const registry = new DecisionRegistry({ config: { ikesuBin: binary }, runAnswer, getDecision, clientViews: views,
    spawnWatch: () => spawnDecisionWatch({ ikesuBin: binary }, () => {}, env), log: () => {} });
  t.after(() => registry.stop());
  await registry.start();
  const decision = (await rpc('decision.create', { kind: 'ask', title: '隔離試験', allow_text: true, options: [{ id: 'yes', label: 'はい', role: 'approve' }] })).decision;
  await until(() => [...registry.entries.values()].some(entry => entry.decision.decision_id === decision.decision_id));
  const id = (await registry.snapshot())[0].id;
  const token = '01'.repeat(32);
  const bridge = await listen({ address: '127.0.0.1', port: 0, token,
    snapshot: () => registry.snapshot(), answer: value => registry.answer(value), later: value => registry.later(value) });
  t.after(() => bridge.close());
  const url = `ws://127.0.0.1:${bridge.address.port}/`;
  const denied = new WebSocket(url);
  await once(denied, 'open');
  const closed = once(denied, 'close');
  denied.send(JSON.stringify({ type: 'auth', token: '00'.repeat(32) }));
  assert.equal((await closed)[0], 1008);
  const noAuth = new WebSocket(url);
  await once(noAuth, 'open');
  let receivedBeforeAuth = 0;
  noAuth.on('message', () => { receivedBeforeAuth++; });
  const noAuthClosed = once(noAuth, 'close');
  noAuth.send(JSON.stringify({ type: 'answer', id, options: ['yes'], text: '', images: [], sequence: 'unauth' }));
  assert.equal((await noAuthClosed)[0], 1008);
  assert.equal(receivedBeforeAuth, 0);
  const oversized = new WebSocket(url);
  await once(oversized, 'open');
  const oversizedClosed = once(oversized, 'close');
  oversized.send(Buffer.alloc(MAX_MESSAGE_BYTES + 1));
  assert.equal((await oversizedClosed)[0], 1009);
  const browser = new WebSocket(url, { origin: 'https://example.test' });
  browser.on('error', () => {});
  await new Promise(resolve => browser.once('close', resolve));
  const phone = new WebSocket(url);
  await once(phone, 'open');
  const initial = once(phone, 'message');
  phone.send(JSON.stringify({ type: 'auth', token }));
  assert.equal(JSON.parse((await initial)[0]).decisions[0].id, id);
  const rejected = once(phone, 'message');
  phone.send(JSON.stringify({ type: 'answer', id, options: ['yes'], text: '', images: [], sequence: 'path', path: '/tmp/forbidden' }));
  assert.equal(JSON.parse((await rejected)[0]).type, 'error');
  const rotated = '02'.repeat(32);
  const phoneClosed = once(phone, 'close');
  bridge.setToken(rotated);
  await phoneClosed;
  const oldToken = new WebSocket(url);
  await once(oldToken, 'open');
  const oldClosed = once(oldToken, 'close');
  oldToken.send(JSON.stringify({ type: 'auth', token }));
  assert.equal((await oldClosed)[0], 1008);
  const newPhone = new WebSocket(url);
  await once(newPhone, 'open');
  const newInitial = once(newPhone, 'message');
  newPhone.send(JSON.stringify({ type: 'auth', token: rotated }));
  assert.equal(JSON.parse((await newInitial)[0]).type, 'snapshot');
  newPhone.close();
  const message = { type: 'answer', id, options: ['yes'], text: '試験', images: [], sequence: 'one' };
  const [first, second] = await Promise.all([registry.answer(message), registry.answer(message)]);
  assert.deepEqual(first, second);
  assert.equal(first.status, 'answered');
  assert.equal(sent, 1);
  const saved = await getDecision('local', decision.decision_id);
  assert.equal(saved.answer.responder, 'android');
  assert.equal(saved.answer.text, '試験');
  await assert.rejects(registry.answer({ ...message, text: '別の文' }), /送信番号/);
  await assert.rejects(registry.answer({ ...message, id: 'unknown', sequence: 'two' }), /見つかりません/);

  const create = async (title, extra = {}) => (await rpc('decision.create', { kind: 'ask', title, options: [{ id: 'yes', label: 'はい', role: 'approve' }], ...extra })).decision;
  const answered = await create('切断中の回答');
  const cancelled = await create('切断中の取消');
  const expired = await create('切断中の期限切れ', { timeout_ms: 100 });
  const pending = await create('残る質問');
  await until(() => registry.entries.size >= 3);
  await rpc('decision.answer', { decision_id: answered.decision_id, option_id: 'yes', responder: 'elsewhere' });
  await rpc('decision.cancel', { decision_id: cancelled.decision_id });
  await until(() => ![...registry.entries.values()].some(entry => [answered.decision_id, cancelled.decision_id, expired.decision_id].includes(entry.decision.decision_id)));
  const reconnect = new WebSocket(url);
  await once(reconnect, 'open');
  const resync = once(reconnect, 'message');
  reconnect.send(JSON.stringify({ type: 'auth', token: rotated }));
  const rows = JSON.parse((await resync)[0]).decisions;
  assert.deepEqual(rows.map(row => row.decision_id), [pending.decision_id]);
  const dropped = { type: 'answer', id: rows[0].id, options: ['yes'], text: '', images: [], sequence: 'lost-reply' };
  const sendsBefore = sent;
  await new Promise((resolve, reject) => reconnect.send(JSON.stringify(dropped), error => error ? reject(error) : resolve()));
  reconnect.terminate();
  await until(() => registry.submissions.has(dropped.sequence));
  await registry.submissions.get(dropped.sequence).result;
  const afterLoss = new WebSocket(url);
  await once(afterLoss, 'open');
  const afterLossSnapshot = once(afterLoss, 'message');
  afterLoss.send(JSON.stringify({ type: 'auth', token: rotated }));
  await afterLossSnapshot;
  const recovered = once(afterLoss, 'message');
  afterLoss.send(JSON.stringify(dropped));
  assert.equal(JSON.parse((await recovered)[0]).status, 'answered');
  assert.equal(sent - sendsBefore, 1);
  afterLoss.close();

  await until(() => views.ready);
  const terminal = net.createConnection(path.join(home, 'state', session, 'ikesu-client.sock'));
  t.after(() => terminal.destroy());
  let terminalData = '';
  terminal.on('data', data => { terminalData += data; });
  await once(terminal, 'connect');
  terminal.write(JSON.stringify({ type: 'hello', protocol: 1, generation: 4, min_generation: 1, cols: 80, rows: 24 }) + '\n');
  await until(() => terminalData.includes('welcome'));
  const view = { machine_id: 'local', session, workspace_id: 's1', pane_ids: ['p1'], focused: true };
  terminal.write(JSON.stringify({ type: 'client_view', client_view: view }) + '\n');
  await until(async () => (await views.list()).some(item => item.focused && item.pane_ids.includes('p1')));
  const byKind = {};
  for (const kind of ['agent_prompt', 'tool_permission', 'ask', 'command_guard'])
    byKind[kind] = await create(`通知条件 ${kind}`, { kind, origin: { pane_id: 'p1', reason: 'screen_prompt' } });
  await until(() => Object.values(byKind).every(decision => [...registry.entries.values()].some(entry => entry.decision.decision_id === decision.decision_id)));
  const visibility = await registry.snapshot();
  for (const kind of Object.keys(byKind)) {
    const row = visibility.find(item => item.decision_id === byKind[kind].decision_id);
    assert.equal(row.notify, kind === 'ask' || kind === 'command_guard', kind);
  }
  views.stop();
  const unavailable = await registry.snapshot();
  assert.equal(unavailable.find(item => item.decision_id === byKind.agent_prompt.decision_id).notify, true);
});
