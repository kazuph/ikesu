import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
const base=process.env.GTX_PANEL_EVIDENCE_DIR;
const json=file=>JSON.parse(fs.readFileSync(file,'utf8'));

test('saved native panel evidence matches real decision origin, single answer and process exit',()=>{
 assert.ok(base,'GTX_PANEL_EVIDENCE_DIR is required');
 for(const group of ['native','native-guard']) {
  const dir=path.join(base,group);
  const history=json(path.join(dir,'live-ssh.json')).transcript;
  const events=json(path.join(dir,'watch.json'));
  const daemon=fs.readFileSync(path.join(dir,'daemon-job.log'),'utf8');
  const records=history.filter(x=>x.nativePanel||x.nativeGuard);
  assert.ok(records.length>0);
  for(const row of records) {
   const proof=row.nativeGuard||row;
   const id=proof.decision;
   const created=events.find(x=>x.event==='created'&&x.decision?.decision_id===id);
   assert.ok(created);
   assert.equal(events.filter(x=>x.event==='resolved'&&x.decision?.decision_id===id).length,1);
   const answers=history.filter(x=>x.args?.[0]==='decision'&&x.args?.[1]==='answer'&&x.args?.[2]===id);
   assert.equal(answers.filter(x=>x.code===0).length,1);
   assert.ok(answers.some(x=>x.code===1&&x.stderr.includes('decision_already_resolved')));
   const spec=proof.spec;
   assert.equal(spec.options.length,created.decision.options.length);
   assert.deepEqual(spec.options.map(x=>x.id),created.decision.options.map(x=>x.id));
   for(const field of ['workspace_label','tab_label','pane_label']) assert.ok(spec.location.includes(created.decision.origin[field]));
   if(created.machine.id==='local')assert.equal(spec.machine,'');
   else assert.equal(spec.machine,created.machine.label);
   assert.ok(spec.details.some(x=>x.label==='ペイン'&&x.value===created.decision.origin.pane_id));
   assert.ok(daemon.includes('ダイアログを閉じます: '+id));
   const imageName=row.nativeGuard?'native-guard':row.nativePanel==='local'?'native-local':'native-mini';
   const png=fs.readFileSync(path.join(dir,imageName+'.png'));
   assert.equal(png.subarray(0,8).toString('hex'),'89504e470d0a1a0a');
   const width=png.readUInt32BE(16),height=png.readUInt32BE(20);assert.ok(width>0&&height>0);
   console.log(JSON.stringify({group,decision:id,machine:created.machine.label,location:spec.location,options:spec.options.length,width,height}));
  }
  assert.equal((daemon.match(/dialog が終了: code=0 signal=null/g)||[]).length,records.length);
 }
});

test('real Claude screen question is answered through the parent and its native process exits',()=>{
 assert.ok(base,'GTX_PANEL_EVIDENCE_DIR is required');
 const dir=path.join(base,'real-screen-native');
 const decision=json(path.join(dir,'answered.json')).result.decision;
 const spec=json(path.join(dir,'native-screen-spec.json'));
 assert.equal(decision.kind,'agent_prompt');assert.equal(decision.origin.reason,'screen_prompt');assert.equal(decision.status,'answered');
 assert.equal(spec.machine,'mini-screen');assert.equal(spec.decision_id,decision.decision_id);assert.equal(spec.options.length,decision.options.length);
 const selected=decision.options.find(x=>x.id===decision.answer.option_id);assert.ok(selected);
 assert.ok(fs.readFileSync(path.join(dir,'actual-screen.txt'),'utf8').includes('→ '+selected.label));
 const events=json(path.join(dir,'watch.json'));assert.equal(events.filter(x=>x.event==='resolved'&&x.decision?.decision_id===decision.decision_id).length,1);
 const log=fs.readFileSync(path.join(dir,'daemon-job.log'),'utf8');assert.ok(log.includes('ダイアログを閉じます: '+decision.decision_id));assert.equal((log.match(/dialog が終了: code=0 signal=null/g)||[]).length,1);
 const png=fs.readFileSync(path.join(dir,'native-screen.png'));assert.equal(png.subarray(0,8).toString('hex'),'89504e470d0a1a0a');
 console.log(JSON.stringify({kind:decision.kind,machine:spec.machine,selected:selected.label,options:spec.options.length,width:png.readUInt32BE(16),height:png.readUInt32BE(20)}));
});
