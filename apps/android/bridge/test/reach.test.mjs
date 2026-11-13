import test from 'node:test';
import assert from 'node:assert/strict';
import { WebSocket } from 'ws';
import { once } from 'node:events';
import { DecisionRegistry } from '../registry.mjs';
import { listen, validateShown, MAX_SHOWN_IDS } from '../socket.mjs';

const machine = { id: 'm1', label: 'mini' };
const decision = id => ({ decision_id: id, kind: 'ask', title: 't', status: 'pending', options: [{ id: 'yes', label: 'はい', role: 'approve' }], origin: {}, created_unix_ms: 1 });
function makeRegistry(overrides = {}) {
  const calls = [];
  const registry = new DecisionRegistry({ config: { laterMs: 0 }, log: () => {}, getDecision: async () => ({}),
    runAnswer: async args => { calls.push(args); return { code: 0, stdout: '', stderr: '' }; }, ...overrides });
  return { registry, calls };
}
const reachCalls = calls => calls.filter(args => args[1] === 'reach');

test('shown の検証: ids は文字列の配列で件数に上限がある', () => {
  assert.deepEqual(validateShown({ type: 'shown', ids: ['a', 'b'] }).ids, ['a', 'b']);
  assert.deepEqual(validateShown({ type: 'shown', ids: [] }).ids, []);
  for (const bad of [{ type: 'shown' }, { type: 'shown', ids: 'a' }, { type: 'shown', ids: [1] }, { type: 'shown', ids: [''] },
    { type: 'shown', ids: ['a'], extra: 1 }, { type: 'shown', ids: Array(MAX_SHOWN_IDS + 1).fill('a') }, { type: 'shown', ids: ['x'.repeat(129)] }]) {
    assert.throws(() => validateShown(bad), /不正/, JSON.stringify(bad).slice(0, 60));
  }
});

test('shown は アプリの id をマシンと判断待ちに戻し、そのマシンのサーバーへ shown を送る', async () => {
  const { registry, calls } = makeRegistry();
  registry.onCreated('m1', machine, decision('dec-1'));
  registry.onCreated('m2', { id: 'm2', label: 'ubuntu' }, decision('dec-2'));
  const snapshot = await registry.snapshot();
  const idOf = decisionId => snapshot.find(item => item.raw_title === 't' && registry.find(item.id).decision.decision_id === decisionId).id;
  await registry.shown([idOf('dec-2'), idOf('dec-2'), 'unknown-id']);
  assert.deepEqual(reachCalls(calls), [['decision', 'reach', 'dec-2', '--channel', 'android', '--state', 'shown', '--machine', 'm2']]);
  // 送れた後は同じ id が来ても繰り返さない。送れなかったときは次のつなぎ直しでもう一度送る。
  await registry.shown([idOf('dec-2')]);
  assert.equal(reachCalls(calls).length, 1);
  registry.runAnswer = async args => { calls.push(args); return { code: 1, stderr: 'boom' }; };
  await registry.shown([idOf('dec-1')]);
  await registry.shown([idOf('dec-1')]);
  assert.equal(reachCalls(calls).filter(args => args[2] === 'dec-1').length, 2);
});

test('スマホの接続が1つも無いまま判断待ちを受け取ると no_client を送り、あとで shown が来れば shown に上がる', async () => {
  const { registry, calls } = makeRegistry();
  registry.hasClients = () => false;
  registry.onCreated('m1', machine, decision('dec-1'));
  registry.onCreated('m1', machine, decision('dec-1'));
  await new Promise(resolve => setImmediate(resolve));
  assert.deepEqual(reachCalls(calls), [['decision', 'reach', 'dec-1', '--channel', 'android', '--state', 'no_client', '--machine', 'm1']]);
  registry.hasClients = () => true;
  await registry.shown([(await registry.snapshot())[0].id]);
  assert.equal(reachCalls(calls).length, 2);
  assert.equal(reachCalls(calls)[1][6], 'shown');
});

test('接続があるときは no_client を送らない', async () => {
  const { registry, calls } = makeRegistry();
  registry.hasClients = () => true;
  registry.onCreated('m1', machine, decision('dec-1'));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(reachCalls(calls).length, 0);
});

test('WebSocket: shown を受け付け、不正な shown と未認証は拒否する。clientCount は認証済みだけ数える', async t => {
  const received = [];
  const token = '01'.repeat(32);
  const bridge = await listen({ address: '127.0.0.1', port: 0, token, snapshot: async () => [], answer: async () => ({}), later: async () => {}, shown: async ids => { received.push(ids); } });
  t.after(() => bridge.close());
  const url = `ws://127.0.0.1:${bridge.address.port}/`;
  const ws = new WebSocket(url);
  await once(ws, 'open');
  assert.equal(bridge.clientCount(), 0);
  const messages = [];
  ws.on('message', bytes => messages.push(JSON.parse(bytes.toString())));
  ws.send(JSON.stringify({ type: 'auth', token }));
  while (!messages.some(message => message.type === 'snapshot')) await new Promise(resolve => setTimeout(resolve, 5));
  assert.equal(bridge.clientCount(), 1);
  ws.send(JSON.stringify({ type: 'shown', ids: ['a', 'b'] }));
  while (received.length < 1) await new Promise(resolve => setTimeout(resolve, 5));
  assert.deepEqual(received, [['a', 'b']]);
  ws.send(JSON.stringify({ type: 'shown', ids: [1] }));
  while (!messages.some(message => message.type === 'error')) await new Promise(resolve => setTimeout(resolve, 5));
  assert.equal(received.length, 1);
  ws.close();
  const anon = new WebSocket(url);
  await once(anon, 'open');
  const closed = once(anon, 'close');
  anon.send(JSON.stringify({ type: 'shown', ids: ['a'] }));
  assert.equal((await closed)[0], 1008);
  assert.equal(received.length, 1);
});
