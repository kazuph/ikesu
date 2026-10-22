import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {MacDialogDecisionsDaemon,spawnHelperDialog,acquireDaemonLock} from '../mac-dialog-decisions.mjs';
const bounded=async promise=>{let timer;try{return await Promise.race([promise,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error('exit/stop did not settle')),1000);})]);}finally{clearTimeout(timer);}};
for(const code of ['ENOENT','EACCES']) {
 test(`実spawn ${code} は終了待ちを失敗として確定する`,async t=>{
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'panel-spawn-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
  const binary=root+'/panel';if(code==='EACCES')fs.writeFileSync(binary,'#!/bin/sh\nexit 0\n',{mode:0o600});
  const child=spawnHelperDialog({helperCmd:[binary]},root+'/spec.json');t.after(()=>child.kill());
  await bounded((async()=>{for await(const _ of child.lines){}})());
  const result=await bounded(child.exited);assert.equal(result.error.code,code);assert.notEqual(result.code,0);
 });
 test(`実spawn ${code} のdaemon停止はspec/result削除と所有ロック解放まで終わる`,async t=>{
  const native=process.env.MAC_DIALOG_TEST_NATIVE_BINARY;assert.ok(native,'MAC_DIALOG_TEST_NATIVE_BINARY required');
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'panel-owner-'));t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
  const binary=root+'/panel';if(code==='EACCES')fs.writeFileSync(binary,'not executable',{mode:0o600});
  const lock=await acquireDaemonLock(native,root);t.after(()=>lock.close());assert.ok(lock);
  const spec=root+'/spec.json',cfg={stateDir:root,helperCmd:[binary]};
  const d=new MacDialogDecisionsDaemon({config:cfg,spawnDialog:p=>spawnHelperDialog(cfg,p),specFilePath:()=>spec,log:()=>{}});
  fs.writeFileSync(spec+'.result.jsonl','previous result');
  d._present({key:'local:test',machineId:'local',machine:{id:'local'},decision:{decision_id:'test',title:'failure',options:[]}});
  try{await bounded(d._dialogDone);assert.equal(d.stopped,true);await bounded(d.stop());}finally{await lock.close();}
  assert.equal(d.children.size,0);assert.equal(fs.existsSync(spec),false);assert.equal(fs.existsSync(spec+'.result.jsonl'),false);
  const next=await acquireDaemonLock(native,root);assert.ok(next);await next.close();
 });
}
