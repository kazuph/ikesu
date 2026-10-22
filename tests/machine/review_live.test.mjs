import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawn,execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {once} from 'node:events';
import {createHash} from 'node:crypto';
import {requireLiveConfig,isolatedEnv,cleanupSteps,reportToParent} from './live_test_support.mjs';
import {MacDialogDecisionsDaemon,createRunAnswer,spawnDecisionWatch,spawnHelperDialog,acquireDaemonLock} from '../../apps/mac-dialog-decisions/mac-dialog-decisions.mjs';
const exec=promisify(execFile),quote=s=>`'${String(s).replaceAll("'","'\\''")}'`;
const until=async fn=>{const deadline=Date.now()+45_000;let error;do{try{return await fn();}catch(e){error=e;}await new Promise(r=>setTimeout(r,50));}while(Date.now()<deadline);throw error;};
const waitExit=async child=>{if(child&&child.exitCode===null&&child.signalCode===null)await once(child,'close');};
const socketIn=root=>{for(const e of fs.readdirSync(root,{withFileTypes:true})){const p=path.join(root,e.name);if(e.isDirectory()){const x=socketIn(p);if(x)return x;}else if(e.isSocket()&&e.name==='ikesu.sock')return p;}return null;};
const configText='onboarding=false\n[experimental]\nallow_nested=true\n[ui.sound]\nenabled=false\n[session]\nresume_agents_on_restore=false\n';

test('review failures use real isolated servers, CLI transports and native panels without clicking', {timeout:240_000},async t=>{
 const live=requireLiveConfig();const native=process.env.IKESU_LIVE_TEST_NATIVE_BINARY||process.env.IKESU_LIVE_TEST_NATIVE_BINARY;assert.ok(native,'required native binary');
 const {BIN:bin,REMOTE_BIN:remoteBin,TARGET:target,EVIDENCE:evidence}=live;
 fs.mkdirSync(evidence,{recursive:true});const root=fs.mkdtempSync(path.join(os.tmpdir(),'gmd-review-'));
 let remoteRoot,localServer,remoteServer,daemon;const requests=[],logs=[],events=[],results={};
 const save=()=>fs.writeFileSync(evidence+'/review-live.json',JSON.stringify({root,remoteRoot,results,events,logs},null,2));
 const env={...isolatedEnv(process.env),HOME:root+'/home',IKESU_STATE_DIR:root+'/state',IKESU_DB_PATH:root+'/dispatch.db',IKESU_CONFIG:root+'/config.toml',AI_GUARD_TEMP_APPROVAL_FILE:root+'/approval'};
 fs.mkdirSync(env.HOME);fs.writeFileSync(env.IKESU_CONFIG,configText);
 const local=async args=>(await exec(bin,['--session','review-proof',...args],{env,timeout:15_000})).stdout;
 const remote=async args=>(await exec('ssh',[target,[remoteRoot+'/ikesu','--session','review-proof',...args].map(quote).join(' ')],{timeout:15_000})).stdout;
 const cleanup=[async()=>{await daemon?.stop();},async()=>{for(const request of requests){request.kill('SIGTERM');await waitExit(request);}},async()=>{if(localServer){try{await local(['server','stop']);}finally{localServer.kill('SIGTERM');await waitExit(localServer);}}},async()=>{if(remoteServer){try{await remote(['server','stop']);}finally{remoteServer.kill('SIGTERM');await waitExit(remoteServer);}}},()=>save(),async()=>{if(remoteRoot)await exec('ssh',[target,'rm -r -- '+quote(remoteRoot)]);},()=>{fs.rmSync(root,{recursive:true,force:true});}];
 t.after(()=>cleanupSteps(cleanup));
 remoteRoot=(await exec('ssh',[target,'mktemp -d /tmp/gmd-review-XXXXXX'])).stdout.trim();env.IKESU_MACHINE_REMOTE_BINARY=remoteRoot+'/ikesu';
 const wrapper=`#!/bin/sh\nexport HOME=${quote(remoteRoot+'/home')}\nexport IKESU_STATE_DIR=${quote(remoteRoot+'/state')}\nexport IKESU_DB_PATH=${quote(remoteRoot+'/dispatch.db')}\nexport IKESU_CONFIG=${quote(remoteRoot+'/config.toml')}\nexport AI_GUARD_TEMP_APPROVAL_FILE=${quote(remoteRoot+'/approval')}\nexec ${quote(remoteBin)} "$@"\n`;
 await exec('ssh',[target,`mkdir ${quote(remoteRoot+'/home')}; cat > ${quote(remoteRoot+'/config.toml')} <<'CONFIG'\n${configText}CONFIG\ncat > ${quote(remoteRoot+'/ikesu')} <<'WRAPPER'\n${wrapper}WRAPPER\nchmod +x ${quote(remoteRoot+'/ikesu')}`]);
 const startRemote=()=>{remoteServer=spawn('ssh',[target,[remoteRoot+'/ikesu','--session','review-proof','server'].map(quote).join(' ')]);remoteServer.stderr.on('data',b=>logs.push('remote: '+b));};
 localServer=spawn(bin,['--session','review-proof','server'],{env});localServer.stderr.on('data',b=>logs.push('local: '+b));startRemote();
 await until(()=>local(['workspace','list']));await until(()=>remote(['workspace','list']));
 await local(['machine','add',target,'--label','review-remote','--remote-session','review-proof']);
 const profile=JSON.parse(await local(['machine','list','--json']))[0];
 const socket=socketIn(root+'/state');assert.ok(socket);env.IKESU_SOCKET_PATH=socket;env.IKESU_SOCKET_EXPLICIT='1';
 const answerEnv={...env};const cfg={ikesuBin:bin,stateDir:root+'/plugin',helperCmd:[native],laterMs:600_000,presentDelayMs:0};fs.mkdirSync(cfg.stateDir);
 let sends=0;
 // Lifetime OS lock: simultaneous owners cannot acquire, and release permits a successor.
 const lock=await acquireDaemonLock(native,cfg.stateDir);assert.ok(lock);
 assert.equal(await acquireDaemonLock(native,cfg.stateDir),null);await lock.close();
 const successor=await acquireDaemonLock(native,cfg.stateDir);assert.ok(successor);await successor.close();results.lock='exclusive, released, reacquired';
 if(process.env.IKESU_LIVE_TEST_REVIEW_ONLY==='ownership') {
  const pluginRoot=path.resolve('apps/mac-dialog-decisions');
  const state=cfg.stateDir+'/run-owner';const hash=createHash('sha256').update(fs.readFileSync(pluginRoot+'/dialog.swift')).digest('hex').slice(0,12);
  fs.mkdirSync(state+'/bin',{recursive:true});fs.copyFileSync(native,state+'/bin/ikesu-decision-dialog-'+hash);fs.chmodSync(state+'/bin/ikesu-decision-dialog-'+hash,0o755);
  const runEnv={...env,IKESU_PLUGIN_STATE_DIR:state,IKESU_PLUGIN_CONFIG_DIR:root+'/plugin-config',IKESU_BIN_PATH:bin};
  const daemons=[0,1].map(()=>spawn(process.execPath,[pluginRoot+'/mac-dialog-decisions.mjs','run'],{env:runEnv}));
  for(const proc of daemons){requests.push(proc);proc.stderr.on('data',b=>logs.push('run: '+b));}
  await until(()=>assert.ok(fs.existsSync(state+'/daemon.json')));
  await until(()=>assert.equal(daemons.filter(p=>p.exitCode===0).length,1));
  const owner=JSON.parse(fs.readFileSync(state+'/daemon.json','utf8'));
  const active=daemons.filter(p=>p.exitCode===null&&p.signalCode===null);assert.equal(active.length,1);assert.equal(active[0].pid,owner.pid);
  assert.equal(await acquireDaemonLock(native,state),null);
  active[0].kill('SIGTERM');await waitExit(active[0]);assert.ok(!fs.existsSync(state+'/daemon.json'));
  const next=await acquireDaemonLock(native,state);assert.ok(next);await next.close();
  results.runOwnership={simultaneousRuns:2,livingOwner:1,ownerPID:owner.pid,ownerState:owner.stateDir,lockReleased:true};save();return;
 }
 const answer=createRunAnswer(cfg,s=>logs.push(s),answerEnv);
 daemon=new MacDialogDecisionsDaemon({config:cfg,spawnWatch:async()=>spawnDecisionWatch(cfg,s=>logs.push(s),env),runAnswer:async args=>{sends++;return answer(args);},spawnDialog:(spec)=>spawnHelperDialog(cfg,spec,s=>logs.push(s)),specFilePath:key=>root+'/plugin/'+key.replaceAll(':','_')+'.json',log:s=>logs.push(s)});
 const original=daemon.onWatchLine.bind(daemon);daemon.onWatchLine=line=>{events.push(line);original(line);};await daemon.start();
 const ask=(owner,title)=>{const args=['--session','review-proof','decision','ask','--title',title,'--option','yes:確認:approve','--option','no:却下:reject'];const request=owner==='local'?spawn(bin,args,{env}):spawn('ssh',[target,[remoteRoot+'/ikesu',...args].map(quote).join(' ')]);requests.push(request);request.stdout.on('data',b=>logs.push('ask: '+b));request.stderr.on('data',b=>logs.push('ask stderr: '+b));return request;};
 const shown=async title=>until(()=>{assert.equal(daemon.child?.entry.decision.title,title);assert.ok(fs.existsSync(daemon.child.specPath+'.result.jsonl'));return daemon.child;});
 let child;
 if(process.env.IKESU_LIVE_TEST_REVIEW_ONLY!=='disconnect') {
 ask('local','レビュー：回答接続の失敗');child=await shown('レビュー：回答接続の失敗');const first=child.entry,firstPID=child.pid;
 answerEnv.IKESU_SOCKET_PATH=root+'/missing-answer.sock';
 assert.equal(await daemon.submitAnswer(first,{optionIds:['yes']}),false);
 assert.equal(first.answerPending,false);assert.equal(daemon.child,child);assert.equal(child.pid,firstPID);
 const pending=JSON.parse(await local(['decision','get',first.decision.decision_id]));assert.equal(pending.result.decision.status,'pending');
 assert.equal(sends,1);results.answerFailure={pending:pending.result.decision.status,panelPID:firstPID,automaticRetries:0};save();
 const image=evidence+'/native-review-failure.png';
 await reportToParent(live,'回答接続だけ失敗させた隔離・無音パネル表示中。同じPIDで未解決のままです。クリックせずパネルだけ撮影し '+image+' に保存してください。保存後に明示的再回答と閉鎖を検証します。');
 await new Promise((resolve,reject)=>{if(fs.existsSync(image))return resolve();const watcher=fs.watch(evidence,()=>{if(fs.existsSync(image)){clearTimeout(timer);watcher.close();resolve();}});const timer=setTimeout(()=>{watcher.close();reject(Error('parent screenshot not delivered'));},180_000);});
 answerEnv.IKESU_SOCKET_PATH=socket;
 assert.equal(await daemon.submitAnswer(first,{optionIds:['no']}),true);
 await until(()=>assert.ok(!fs.existsSync(child.specPath)&&!fs.existsSync(child.specPath+'.result.jsonl')));
 const final=JSON.parse(await local(['decision','get',first.decision.decision_id]));assert.equal(final.result.decision.answer.option_id,'no');assert.equal(sends,2);
 results.manualRetry={answer:final.result.decision.answer.option_id,sends,panelPID:firstPID};
 ask('local','レビュー：他所のCLI回答');child=await shown('レビュー：他所のCLI回答');
 await local(['decision','answer',child.entry.decision.decision_id,'yes','--machine','local']);
 await until(()=>assert.ok(!fs.existsSync(child.specPath)&&!fs.existsSync(child.specPath+'.result.jsonl')));results.cliCleanup={spec:false,result:false};
 }
 ask('remote','レビュー：切断時の古い回答');child=await shown('レビュー：切断時の古い回答');const stale=child.entry;
 await remote(['server','stop']);await waitExit(remoteServer);
 await until(()=>assert.ok(daemon.machinesDown.has(profile.id)&&!daemon.entries.has(stale.key)));
 const before=sends;assert.equal(await daemon.submitAnswer(stale,{optionIds:['yes']}),false);assert.equal(sends,before);
 startRemote();await until(()=>remote(['workspace','list']));
 // Reconnection replays the same pending decision into a fresh entry.
 const current=await shown('レビュー：切断時の古い回答');
 assert.notEqual(current.entry,stale);assert.equal(current.entry.key,stale.key);
 assert.equal(await daemon.submitAnswer(stale,{optionIds:['yes']}),false);assert.equal(sends,before);
 assert.equal(await daemon.submitAnswer(current.entry,{optionIds:['no']}),true);
 await until(()=>assert.ok(!fs.existsSync(current.specPath)&&!fs.existsSync(current.specPath+'.result.jsonl')));
 const remoteFinal=JSON.parse(await remote(['decision','get',current.entry.decision.decision_id]));assert.equal(remoteFinal.result.decision.answer.option_id,'no');
 results.disconnect={staleSent:false,answer:remoteFinal.result.decision.answer.option_id,sendsBefore:before,sendsAfter:sends};save();
});
