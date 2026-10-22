import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
const root=process.env.IKESU_PANEL_EVIDENCE_DIR;
const read=folder=>{assert.ok(root,'IKESU_PANEL_EVIDENCE_DIR required');return JSON.parse(fs.readFileSync(path.join(root,folder,'review-live.json'),'utf8'));};
test('実サーバーの送信失敗・同じパネルの再回答・CLI後の独立削除の保存証拠',()=>{
 const proof=read('review-live');
 assert.equal(proof.results.answerFailure.pending,'pending');assert.equal(proof.results.answerFailure.automaticRetries,0);
 assert.equal(proof.results.manualRetry.panelPID,proof.results.answerFailure.panelPID);
 assert.equal(proof.results.manualRetry.answer,'no');assert.equal(proof.results.manualRetry.sends,2);
 assert.deepEqual(proof.results.cliCleanup,{spec:false,result:false});
 assert.ok(proof.logs.some(x=>x.includes('server_not_running')));
 assert.ok(proof.logs.filter(x=>x==='dialog が終了: code=0 signal=null').length>=2);
 const image=fs.readFileSync(path.join(root,'review-live/native-review-failure.png'));
 assert.equal(image.subarray(1,4).toString(),'PNG');assert.ok(image.readUInt32BE(16)>0);assert.ok(image.readUInt32BE(20)>0);
});
test('実SSHの切断・同じID再接続・別選択では古い回答を送らない保存証拠',()=>{
 const proof=read('review-disconnect');
 assert.equal(proof.results.disconnect.staleSent,false);assert.equal(proof.results.disconnect.answer,'no');
 assert.equal(proof.results.disconnect.sendsAfter,proof.results.disconnect.sendsBefore+1);
 const down=proof.events.findIndex(e=>e.event==='machine_down');assert.ok(down>=0);
 const original=proof.events.slice(0,down).find(e=>e.event==='created');
 assert.ok(proof.events.slice(down+1).some(e=>e.event==='created'&&e.decision.decision_id===original.decision.decision_id));
});
test('実runの同時起動は一つの所有者だけを維持し終了時に解放する保存証拠',()=>{
 const proof=read('review-ownership');assert.equal(proof.results.runOwnership.simultaneousRuns,2);
 assert.equal(proof.results.runOwnership.livingOwner,1);assert.equal(proof.results.runOwnership.lockReleased,true);
});
