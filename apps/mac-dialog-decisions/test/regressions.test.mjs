import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import * as plugin from '../mac-dialog-decisions.mjs';
const nodeSource=fs.readFileSync(new URL('../mac-dialog-decisions.mjs',import.meta.url),'utf8');
const swift=fs.readFileSync(new URL('../dialog.swift',import.meta.url),'utf8');
const temporary=t=>{const root=fs.mkdtempSync(path.join(os.tmpdir(),'mac-dialog-unit-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));return root;};
function daemon() {return new plugin.MacDialogDecisionsDaemon({config:{},log:()=>{}});}

test('回答失敗を同じ未解決entryに残し、手動再回答を可能にする',()=>{
 assert.match(swift,/answerPending/);
 const answer=swift.slice(swift.indexOf('func answer(ids:'),swift.indexOf('@objc func submit'));
 assert.doesNotMatch(answer,/finish\(/);
 assert.match(swift,/standardInput/);
 assert.match(nodeSource,/child\.respond\(/);
 assert.doesNotMatch(nodeSource,/answerRetryTimer\s*=\s*setTimeout/);
});
test('specが先に消えてもresultを独立に削除する',t=>{
 const root=temporary(t),spec=path.join(root,'decision.json');fs.writeFileSync(spec+'.result.jsonl','private answer');
 assert.equal(typeof plugin.removeDialogFiles,'function');plugin.removeDialogFiles(spec);
 assert.equal(fs.existsSync(spec),false);assert.equal(fs.existsSync(spec+'.result.jsonl'),false);
});
test('空回答のCommand-Enterは無音で画面内の不足表示になる',()=>{
 assert.doesNotMatch(swift,/NSSound\.beep/);assert.match(swift,/選択肢を選ぶか、回答を入力/);
});
test('runは生存期間のOS所有ロックを取り、曖昧なpgrepを使わない',()=>{
 assert.match(swift,/flock\(/);assert.match(nodeSource,/--daemon-lock/);assert.doesNotMatch(nodeSource,/"pgrep"/);
 assert.match(swift,/daemon_owner/);
});
test('切断と同じIDのentry差替え後に古い回答を送らない',async()=>{
 const d=daemon();const entry={key:'mini dec-test',machineId:'mini',decision:{decision_id:'dec-test'},resolved:false,answerPending:false};
 d.entries.set(entry.key,{...entry});assert.equal(await d.submitAnswer(entry,{optionIds:['yes']}),false);assert.equal(entry.answerPending,false);
});
test('切断時は予約済みの古いタイマーを解除する',async t=>{
 const root=temporary(t),marker=path.join(root,'stale-send');const d=daemon();
 const entry={key:'mini dec-test',machineId:'mini',decision:{decision_id:'dec-test'}};
 entry.answerRetryTimer=setTimeout(()=>fs.writeFileSync(marker,'sent'),10);d.entries.set(entry.key,entry);d.onMachineDown('mini','disconnect');
 await new Promise(resolve=>setTimeout(resolve,30));assert.equal(fs.existsSync(marker),false);
});
test('実SSH試験は親宛先と接続先の明示なしでは外部処理前に拒否する',async()=>{
 const support=await import('../../../tests/machine/live_test_support.mjs');
 assert.throws(()=>support.requireLiveConfig({}, {screen:true}),/required/);
 for(const filename of ['live_ssh_decisions.test.mjs','live_screen_prompt.test.mjs']) {
  const text=fs.readFileSync(new URL('../../../tests/machine/'+filename,import.meta.url),'utf8');
  assert.doesNotMatch(text,/['"]p4['"]|['"]mini['"]|\/Users\/kazuph\//);
 }
});
test('IKESU環境を読み、消えた旧CLIの代わりにikesuを先に探す',t=>{
 const root=temporary(t);fs.symlinkSync(process.execPath,path.join(root,'ikesu'));
 const config=plugin.loadConfig({IKESU_PLUGIN_CONFIG_DIR:root+'/config',IKESU_PLUGIN_STATE_DIR:root+'/state',IKESU_BIN_PATH:root+'/removed'});
 assert.equal(config.configDir,root+'/config');assert.equal(config.stateDir,root+'/state');
 assert.equal(plugin.resolveIkesuBin(config,()=>{}, {PATH:root}),root+'/ikesu');
});

test('後片付けは先行する失敗後も自分の資源だけを削除する',async t=>{
 const {cleanupSteps}=await import('../../../tests/machine/live_test_support.mjs');
 const root=temporary(t),owned=path.join(root,'owned'),unrelated=path.join(root,'unrelated');
 fs.mkdirSync(owned);fs.writeFileSync(unrelated,'preserve');
 await assert.rejects(cleanupSteps([()=>{throw Error('setup failed');},()=>fs.rmSync(owned,{recursive:true})]),/owned test resource cleanup failed/);
 assert.equal(fs.existsSync(owned),false);assert.equal(fs.readFileSync(unrelated,'utf8'),'preserve');
});
