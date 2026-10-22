import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import net from 'node:net';
import {spawn,execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {MacDialogDecisionsDaemon,spawnDecisionWatch,createGetDecision} from '../mac-dialog-decisions.mjs';
import {manageDaemon,requestSwitch} from '../lifecycle.mjs';
const exec=promisify(execFile);
async function until(predicate){const end=Date.now()+10000;while(!predicate()){if(Date.now()>end)throw Error('real server condition deadline');await new Promise(r=>setTimeout(r,10));}}
async function fixture(t){
 const binary=process.env.MAC_DIALOG_TEST_IKESU;assert.ok(binary,'real ikesu binary required');
 const root=fs.mkdtempSync('/tmp/ikesu-reconcile-'),stateDir=path.join(root,'plugin-state');fs.mkdirSync(stateDir);
 const config=path.join(root,'config.toml');fs.writeFileSync(config,'[experimental]\nallow_nested=true\n');
 const env={...process.env,HOME:root,IKESU_CONFIG:config,IKESU_STATE_DIR:path.join(root,'state'),IKESU_SOCKET_PATH:path.join(root,'state/default/ikesu.sock'),IKESU_SOCKET_PATH_EXPLICIT:'1',SHELL:'/bin/sh'};
 const server=spawn(binary,['server'],{env,stdio:['ignore','ignore','pipe']});let stderr='';server.stderr.on('data',b=>stderr+=b);
 const exited=new Promise(resolve=>server.once('close',resolve));
 const rpc=(method,params={})=>new Promise((resolve,reject)=>{
  const socket=net.createConnection(env.IKESU_SOCKET_PATH);let data='';
  socket.on('connect',()=>socket.write(JSON.stringify({id:'reconcile-test',method,params})+'\n'));
  socket.on('error',reject);socket.on('data',b=>{data+=b;if(!data.includes('\n'))return;socket.end();const reply=JSON.parse(data.split('\n')[0]);reply.error?reject(Error(JSON.stringify(reply.error))):resolve(reply.result);});
 });
 await until(()=>{if(server.exitCode!==null)throw Error(stderr);return fs.existsSync(env.IKESU_SOCKET_PATH)});
 const watches=[],logs=[];let answerSends=0;
 const daemon=new MacDialogDecisionsDaemon({config:{stateDir,presentDelayMs:60000},log:message=>{logs.push(message);t.diagnostic(message)},
  getDecision:createGetDecision({ikesuBin:binary},()=>{},env),
  runAnswer:async()=>{answerSends++;throw Error('unexpected additional answer')},
 });
 async function watch(){
  const handle=spawnDecisionWatch({ikesuBin:binary},()=>{},env);
  const done=(async()=>{try{for await(const row of handle.lines)daemon.onWatchLine(row)}catch{}})();
  const item={handle,done};watches.push(item);return item;
 }
 t.after(async()=>{for(const w of watches){w.handle.close();await w.done}await daemon.stop();try{await rpc('server.stop')}catch{}await exited;fs.rmSync(root,{recursive:true,force:true});});
 const create=async title=>(await rpc('decision.create',{kind:'ask',title,allow_text:true,options:[{id:'yes',label:'Yes',role:'approve'}]})).decision;
 return {root,stateDir,env,exited,logs,daemon,rpc,watch,create,get answerSends(){return answerSends}};
}

test('実照会が切れたentryは保持し次の再接続で保存済みの既決だけ回収する',async t=>{
 const f=await fixture(t),decision=await f.create('lookup disconnect');
 f.daemon.onWatchLine({event:'created',machine:{id:'local'},decision});
 await f.rpc('decision.cancel',{decision_id:decision.decision_id});
 f.daemon.onMachineDown('local','disconnect');
 const hidden=f.env.IKESU_SOCKET_PATH+'.held';fs.renameSync(f.env.IKESU_SOCKET_PATH,hidden);
 try{
  f.daemon.onWatchLine({event:'machine_up',machine:{id:'local'}});
  await until(()=>f.logs.some(line=>line.includes('保持します')));
  assert.equal(f.daemon.entries.size,1);assert.equal([...f.daemon.entries.values()][0].machineDown,true);
 }finally{fs.renameSync(hidden,f.env.IKESU_SOCKET_PATH)}
 f.daemon.onMachineDown('local','reconnect');f.daemon.onWatchLine({event:'machine_up',machine:{id:'local'}});
 await until(()=>f.daemon.entries.size===0);assert.equal(f.answerSends,0);
});

test('decision getの指定なしとlocal指定は同じ保存済み出力で不明マシンは明示エラーになる',async t=>{
 const f=await fixture(t),decision=await f.create('CLI saved lookup');
 const binary=process.env.MAC_DIALOG_TEST_IKESU,args=['decision','get',decision.decision_id];
 const plain=JSON.parse((await exec(binary,args,{env:f.env})).stdout);
 const local=JSON.parse((await exec(binary,[...args,'--machine','local'],{env:f.env})).stdout);
 assert.deepEqual(local,plain);
 await assert.rejects(exec(binary,[...args,'--machine','not-saved'],{env:f.env}),error=>error.code===1&&/machine/.test(error.stderr));
});

test('Astra最小再現のイベント列は実保存先の既決照合後にentryを回収する',async t=>{
 const f=await fixture(t),decision=await f.create('Astra minimum');
 await f.rpc('decision.answer',{decision_id:decision.decision_id,option_id:'yes'});
 f.daemon.onWatchLine({event:'created',machine:{id:'local'},decision});
 f.daemon.onWatchLine({event:'machine_down',machine:{id:'local'}});
 f.daemon.onWatchLine({event:'machine_up',machine:{id:'local'}});
 clearTimeout(f.daemon._pumpTimer);
 await until(()=>f.daemon.entries.size===0);
 assert.equal(f.answerSends,0);
});

test('購読だけ切断中の別クライアント回答と取消を回収しpending状態を保って1回だけ切り替える',async t=>{
 const f=await fixture(t),decisions=[];
 for(const title of ['answered elsewhere','cancelled elsewhere','pending','snoozed','in flight','retrying'])decisions.push(await f.create(title));
 const first=await f.watch();await until(()=>f.daemon.entries.size===decisions.length);
 first.handle.close();await first.done;f.daemon.onMachineDown('local','actual subscription closed');
 const entry=id=>f.daemon.entries.get('local:'+id)||[...f.daemon.entries.values()].find(e=>e.decision.decision_id===id);
 const answered=entry(decisions[0].decision_id),cancelled=entry(decisions[1].decision_id);
 answered.retry={optionIds:['yes'],text:'must not resend'};answered.answerRetryTimer=setTimeout(()=>{throw Error('resolved retry survived')},60000);
 cancelled.dismissed=true;cancelled.snoozeTimer=setTimeout(()=>{throw Error('resolved snooze survived')},60000);
 const pending=decisions.slice(2).map(d=>entry(d.decision_id));
 pending[0].draft={option_ids:['yes'],text:'keep draft'};pending[1].dismissed=true;pending[2].answerPending=true;pending[3].retry={optionIds:['yes'],text:'keep retry'};
 await f.rpc('decision.answer',{decision_id:decisions[0].decision_id,option_id:'yes'});
 await f.rpc('decision.cancel',{decision_id:decisions[1].decision_id});
 const running=path.join(f.root,'old'),next=path.join(f.root,'next'),registry=path.join(f.root,'registration.json');
 fs.mkdirSync(running);fs.mkdirSync(next);fs.writeFileSync(path.join(running,'VERSION'),'0.1.0\n');fs.writeFileSync(registry,JSON.stringify({root:next}));
 let switches=0;
 const lifecycle=manageDaemon(f.daemon,{stateDir:f.stateDir,daemonOwner:'real-reconnect'},{root:running,registeredRoot:async()=>JSON.parse(fs.readFileSync(registry)).root,onSwitch:async()=>{switches++}});t.after(()=>lifecycle.stop());
 requestSwitch(f.stateDir,next);await f.watch();
 await until(()=>f.daemon.entries.size===pending.length&&pending.every(e=>!e.machineDown));
 assert.equal(answered.retry,null);assert.equal(answered.answerRetryTimer,null);assert.equal(cancelled.snoozeTimer,null);
 assert.deepEqual(pending[0].draft,{option_ids:['yes'],text:'keep draft'});assert.equal(pending[1].dismissed,true);assert.equal(pending[2].answerPending,true);assert.equal(pending[3].retry.text,'keep retry');
 assert.equal(f.answerSends,0);assert.equal(switches,0);
 for(const decision of decisions.slice(2))await f.rpc('decision.cancel',{decision_id:decision.decision_id});
 await until(()=>switches===1);assert.equal(f.daemon.entries.size,0);await lifecycle.check();assert.equal(switches,1);assert.equal(f.answerSends,0);
});
