import test from 'node:test';
import assert from 'node:assert/strict';
import {execFile,spawn} from 'node:child_process';
import {promisify} from 'node:util';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {createHash} from 'node:crypto';
import readline from 'node:readline';
import {requireLiveConfig,reportToParent,cleanupSteps,isolatedEnv} from './live_test_support.mjs';
const exec=promisify(execFile);
const quote=s=>`'${String(s).replaceAll("'","'\\''")}'`;

test('real remote Claude question screen reaches the decision API', {timeout:360_000},async t=>{
 const live=requireLiveConfig(process.env,{screen:true});
 const {TARGET:target,REMOTE_BIN:remoteBin,REMOTE_HOME:remoteHome,CLAUDE_BIN:claudeBin,BIN:bin,EVIDENCE:evidence}=live;
 assert.ok(evidence);fs.mkdirSync(evidence,{recursive:true});
 const localRoot=fs.mkdtempSync(path.join(os.tmpdir(),'gmd-real-'));
 let root;
 const cleanups=[()=>fs.rmSync(localRoot,{recursive:true,force:true}),async()=>{if(root)await exec('ssh',[target,'rm -r -- '+quote(root)]);} ];
 t.after(()=>cleanupSteps(cleanups));
 root=(await exec('ssh',[target,'mktemp -d /tmp/gmd-screen-XXXXXX'])).stdout.trim();
 const config='onboarding=false\n[experimental]\nallow_nested=true\n[ui.sound]\nenabled=false\n[session]\nresume_agents_on_restore=false\n';
 const script=`mkdir -p ${quote(root+'/home')}\ncat > ${quote(root+'/config.toml')} <<'CONFIG'\n${config}CONFIG\ncat > ${quote(root+'/ikesu')} <<'WRAPPER'\n#!/bin/sh\nexport HOME=${quote(remoteHome)}\nexport AI_GUARD_TEMP_APPROVAL_FILE=${quote(root+'/approval')}\nexport IKESU_DB_PATH=${quote(root+'/dispatch.db')}\nexport IKESU_STATE_DIR=${quote(root+'/state')}\nexport IKESU_CONFIG=${quote(root+'/config.toml')}\nexec ${quote(remoteBin)} "$@"\nWRAPPER\nchmod +x ${quote(root+'/ikesu')}`;
 await exec('ssh',[target,script]);
 const command=args=>[root+'/ikesu','--session','screen-proof',...args].map(quote).join(' ');
 const cli=async args=>(await exec('ssh',[target,command(args)],{timeout:30_000})).stdout;
 let server,localServer;
 cleanups.unshift(async()=>cleanupSteps([async()=>{if(localServer){try{await local(['server','stop']);}finally{localServer.kill('SIGTERM');}}},async()=>{if(server){try{await cli(['server','stop']);}finally{server.kill('SIGTERM');}}}]));
 server=spawn('ssh',[target,command(['server'])]);
 let serverLog='';server.stderr.on('data',b=>serverLog+=b);
 const localEnv={...isolatedEnv(process.env),AI_GUARD_TEMP_APPROVAL_FILE:localRoot+'/approval',HOME:localRoot+'/home',IKESU_STATE_DIR:localRoot+'/state',IKESU_DB_PATH:localRoot+'/dispatch.db',IKESU_CONFIG:localRoot+'/config.toml',IKESU_MACHINE_REMOTE_BINARY:root+'/ikesu'};
 for(const key of ['IKESU_SOCKET_PATH','IKESU_PANE_ID','IKESU_TAB_ID','IKESU_WORKSPACE_ID','CODEX_THREAD_ID','CLAUDECODE'])delete localEnv[key];
 fs.mkdirSync(localRoot+'/home');fs.writeFileSync(localRoot+'/config.toml',config);
 localServer=spawn(bin,['--session','screen-proof','server'],{env:localEnv});
 const local=async args=>(await exec(bin,['--session','screen-proof',...args],{env:localEnv,timeout:30_000})).stdout;
 const nativeState=localRoot+'/state/plugins/kazuph.mac-dialog-decisions';
 const pluginRoot=path.resolve(path.dirname(new URL(import.meta.url).pathname),'../../apps/mac-dialog-decisions');
 const hash=createHash('sha256').update(fs.readFileSync(pluginRoot+'/dialog.swift')).digest('hex').slice(0,12);
 fs.mkdirSync(nativeState+'/bin',{recursive:true});fs.copyFileSync(live.NATIVE_BINARY,nativeState+'/bin/ikesu-decision-dialog-'+hash);fs.chmodSync(nativeState+'/bin/ikesu-decision-dialog-'+hash,0o755);
 let pane,watch;const lines=[];
 cleanups.unshift(async()=>{
  if(pane){try{fs.writeFileSync(evidence+'/actual-screen.txt',await cli(['pane','read',pane,'--lines','100']));}catch{}}
  watch?.kill('SIGTERM');
  try{await local(['server','stop']);}catch{}
  localServer.kill('SIGTERM');
  fs.writeFileSync(evidence+'/watch.json',JSON.stringify(lines,null,2));
  for(const file of fs.readdirSync(localRoot+'/state/sessions/screen-proof/job-logs',{throwIfNoEntry:false})||[]) {
   if(file.includes('mac-dialog'))fs.copyFileSync(localRoot+'/state/sessions/screen-proof/job-logs/'+file,evidence+'/daemon-job.log');
  }
  fs.copyFileSync(localRoot+'/config.toml',evidence+'/local-config.toml');
  fs.writeFileSync(evidence+'/remote-config.toml',await exec('ssh',[target,'cat '+quote(root+'/config.toml')]).then(x=>x.stdout));
  await cli(['server','stop']);
  if(server.exitCode===null&&server.signalCode===null)await new Promise(resolve=>server.once('close',resolve));
  fs.writeFileSync(evidence+'/environment.json',JSON.stringify({root,localRoot,serverLog},null,2));
 });
 // This probe waits on a real API connection; no screen client is attached.
 const start=Date.now();let ready=false;
 while(!ready&&Date.now()-start<15_000){try{await cli(['workspace','list']);ready=true;}catch{await new Promise(r=>setTimeout(r,100));}}
 assert.ok(ready,'isolated server readiness');
 const until=async(fn)=>{let err;const end=Date.now()+90_000;do{try{return await fn();}catch(e){err=e;}await new Promise(r=>setTimeout(r,100));}while(Date.now()<end);throw err;};
 await until(()=>local(['workspace','list']));
 await local(['machine','add',target,'--label','mini-screen','--remote-session','screen-proof']);
 await local(['plugin','link',pluginRoot]);await local(['plugin','action','invoke','start','--plugin','kazuph.mac-dialog-decisions']);
 watch=spawn(bin,['--session','screen-proof','decision','watch','--all-machines'],{env:localEnv});
 readline.createInterface({input:watch.stdout}).on('line',s=>lines.push(JSON.parse(s)));

 const launched=JSON.parse(await cli(['agent','start','screen-proof','--cwd',root,'--no-focus','--',claudeBin,'--model','claude-opus-5-5','--permission-mode','auto','これは画面検出の短いテストです。ファイル編集とコマンドはせず、AskUserQuestionで「テスト：飲み物は？」を選択肢「コーヒー」「お茶」で質問してください。回答後は1行で終了してください。']));
 pane=launched.result.agent.pane_id;fs.writeFileSync(evidence+'/launch.json',JSON.stringify(launched,null,2));
 try{await cli(['wait','output',pane,'--match','(trust|飲み物|Select login method)','--regex','--source','visible','--timeout','15000']);}catch{}
 let screen=await cli(['pane','read',pane,'--lines','100']);
 fs.writeFileSync(evidence+'/initial-screen.txt',screen);
 if(/trust/i.test(screen)) {
  assert.ok(screen.includes(root)||screen.includes(root.replace('/tmp/','/private/tmp/')),'only the authorized temporary folder may be trusted');
  if(screen.includes('❯ No, exit')) await cli(['pane','send-keys',pane,'Down']);
  await cli(['pane','send-keys',pane,'Enter']);
 }
 await until(async()=>assert.ok(lines.some(x=>x.event==='created'&&x.decision?.kind==='agent_prompt')));
 screen=await cli(['pane','read',pane,'--lines','100']);fs.writeFileSync(evidence+'/actual-screen.txt',screen);
 const decisions=JSON.parse(await cli(['decision','list','--status','pending']));fs.writeFileSync(evidence+'/decisions.json',JSON.stringify(decisions,null,2));
 const record=decisions.result.decisions.find(x=>x.kind==='agent_prompt');
 assert.ok(record,'real question must be reached; inspect actual-screen.txt for human setup/authentication blockers');
 let specPath,spec;
 await until(async()=>{const dir=nativeState+'/dialogs';specPath=fs.readdirSync(dir).map(x=>dir+'/'+x).find(x=>JSON.parse(fs.readFileSync(x,'utf8')).decision_id===record.decision_id);assert.ok(specPath);spec=JSON.parse(fs.readFileSync(specPath,'utf8'));});
 assert.equal(spec.machine,'mini-screen');assert.equal(spec.options.length,record.options.length);
 fs.writeFileSync(evidence+'/native-screen-spec.json',JSON.stringify(spec,null,2));
 const destination=evidence+'/native-screen.png';
 const message='実Claude質問由来の隔離Macパネル表示中。クリックせず撮影し '+destination+' に保存してください。保存後CLIで回答します。';
 await reportToParent(live,message);
 await new Promise((resolve,reject)=>{if(fs.existsSync(destination)){resolve();return;}const timer=setTimeout(()=>{watcher.close();reject(Error('screenshot not delivered'));},180_000);const watcher=fs.watch(evidence,()=>{if(fs.existsSync(destination)){clearTimeout(timer);watcher.close();resolve();}});});
 await local(['decision','answer',record.decision_id,record.options[0].id,'--machine','mini-screen']);
 await until(async()=>{assert.ok(!fs.existsSync(specPath));assert.ok(!fs.existsSync(specPath+'.result.jsonl'));});
 const answered=JSON.parse(await cli(['decision','get',record.decision_id]));assert.equal(answered.result.decision.answer.option_id,record.options[0].id);
 fs.writeFileSync(evidence+'/answered.json',JSON.stringify(answered,null,2));
});
