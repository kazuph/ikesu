import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawn, execFile} from 'node:child_process';
import {promisify} from 'node:util';
import readline from 'node:readline';
import {createHash} from 'node:crypto';
const exec = promisify(execFile);
const quote = s => `'${String(s).replaceAll("'", "'\\''")}'`;
const bin = process.env.GTX_LIVE_TEST_BIN;
const remoteBin = process.env.GTX_LIVE_TEST_REMOTE_BIN;
const target = process.env.GTX_LIVE_TEST_TARGET;
const evidence = process.env.GTX_LIVE_TEST_EVIDENCE;
const configText = 'onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n[ui.sound]\nenabled=false\n';

test('real SSH saved machine, pane operation, remote run, ask and guard answers', {timeout:600_000}, async t => {
 assert.ok(bin && remoteBin && target && evidence, 'all GTX_LIVE_TEST_* prerequisites must be explicit');
 fs.mkdirSync(evidence,{recursive:true});
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'gmd-'));
 const remoteRoot=(await exec('ssh',[target,'mktemp -d /tmp/gmd-XXXXXX'])).stdout.trim();
 fs.mkdirSync(path.join(root,'home'));
 const cfg=path.join(root,'config.toml'); fs.writeFileSync(cfg,configText);
 const env={...process.env,HOME:path.join(root,'home'),AI_GUARD_TEMP_APPROVAL_FILE:path.join(root,'approval'),GTX_STATE_DIR:path.join(root,'state'),GTX_DB_PATH:path.join(root,'dispatch.db'),GTX_CONFIG:cfg,GTX_MACHINE_REMOTE_BINARY:remoteRoot+'/gtx'};
 for(const k of ['GTX_SOCKET_PATH','GTX_PANE_ID','GTX_TAB_ID','GTX_WORKSPACE_ID','HERDR_PANE_ID','CODEX_THREAD_ID','CLAUDECODE']) delete env[k];
 const transcript=[];
 const run=async(args,allowFail=false)=>{
  try { const r=await exec(bin,['--session','proof',...args],{env,timeout:15_000}); transcript.push({args,code:0,...r}); return r.stdout; }
  catch(e) { transcript.push({args,code:e.code,stdout:e.stdout,stderr:e.stderr}); if(!allowFail) throw e; return e; }
 };
 const ssh=async args=>(await exec('ssh',[target,[remoteRoot+'/gtx','--session','proof',...args].map(quote).join(' ')],{timeout:15_000})).stdout;
 const setup=spawn('ssh',[target,`mkdir -p ${quote(remoteRoot+'/home')} && cat > ${quote(remoteRoot+'/config.toml')} && cat > ${quote(remoteRoot+'/gtx')} <<'WRAPPER'\n#!/bin/sh\nexport HOME=${quote(remoteRoot+'/home')}\nexport AI_GUARD_TEMP_APPROVAL_FILE=${quote(remoteRoot+'/approval')}\nexport GTX_STATE_DIR=${quote(remoteRoot+'/state')}\nexport GTX_DB_PATH=${quote(remoteRoot+'/dispatch.db')}\nexport GTX_CONFIG=${quote(remoteRoot+'/config.toml')}\nexec ${quote(remoteBin)} "$@"\nWRAPPER\nchmod +x ${quote(remoteRoot+'/gtx')}`]);
 setup.stdin.end(configText); await new Promise((r,j)=>setup.on('exit',c=>c===0?r():j(Error('remote setup '+c))));
 const local=spawn(bin,['--session','proof','server'],{env,stdio:['ignore','pipe','pipe']});
 const remote=spawn('ssh',[target,`${quote(remoteRoot+'/gtx')} --session proof server`],{stdio:['ignore','pipe','pipe']});
 let localLog='',remoteLog=''; local.stderr.on('data',b=>localLog+=b);remote.stderr.on('data',b=>remoteLog+=b);
 let watch,ask,guard,nativeState;const nativeRequests=[]; const lines=[];let guardOutput='',guardErr='';
 t.after(async()=>{
  if(nativeState) {try {const pid=JSON.parse(fs.readFileSync(path.join(nativeState,'daemon.json'),'utf8')).pid;process.kill(pid,'SIGTERM');}catch{}}
  for(const request of nativeRequests) request.kill('SIGTERM');
  watch?.kill('SIGTERM'); ask?.kill('SIGTERM'); guard?.kill('SIGTERM');
  try {await run(['server','stop']);}catch{}
  try {await ssh(['server','stop']);}catch{}
  local.kill('SIGTERM');remote.kill('SIGTERM');
  fs.writeFileSync(path.join(evidence,'watch.json'),JSON.stringify(lines,null,2));
  fs.writeFileSync(path.join(evidence,'guard.json'),JSON.stringify({guardOutput,guardErr},null,2));
  fs.writeFileSync(path.join(evidence,'live-ssh.json'),JSON.stringify({root,remoteRoot,transcript,localLog,remoteLog},null,2));
 });
 const until=async(fn,limit=15_000)=>{const end=Date.now()+limit;let last;do{try{return await fn();}catch(e){last=e;} await new Promise(r=>setTimeout(r,100));}while(Date.now()<end);throw last;};
 await until(()=>run(['workspace','list']));await until(()=>ssh(['workspace','list']));
 await run(['machine','add',target,'--label','mini-proof','--remote-session','proof']);
 const profiles=JSON.parse(await run(['machine','list','--json'])); assert.equal(profiles.length,1);assert.equal(profiles[0].target,target);
 const workspace=JSON.parse(await run(['--machine','mini-proof','workspace','create','--cwd','/tmp','--label','テスト mini','--no-focus']));
 transcript.push({remoteWorkspace:workspace});
 const panes=JSON.parse(await run(['--machine','mini-proof','pane','list']));
 const pane=panes.result.panes.at(-1).pane_id;
 await run(['--machine','mini-proof','pane','send-text',pane,"printf 'REMOTE_%s\\n' PANE_OK"]);
 await run(['--machine','mini-proof','pane','send-keys',pane,'Enter']);
 await until(async()=>{const text=await run(['--machine','mini-proof','pane','read',pane,'--lines','30']);assert.match(text,/REMOTE_PANE_OK/);});
 const job=JSON.parse(await run(['run','--caller','p1','--machine','mini-proof','--cwd','/tmp','--label','remote-proof','--completion','none','--','printf','REMOTE_RUN_OK\n']));
 assert.ok(job.job);
 watch=spawn(bin,['--session','proof','decision','watch','--all-machines'],{env});
 readline.createInterface({input:watch.stdout}).on('line',line=>{lines.push(JSON.parse(line));});
 await until(async()=>assert.ok(lines.some(x=>x.event==='machine_up'&&x.machine.id===profiles[0].id)));
 ask=spawn('ssh',[target,[remoteRoot+'/gtx','--session','proof','decision','ask','--title','テスト mini SSH ask','--option','yes:確認:approve','--option','no:却下:reject'].map(quote).join(' ')]);
 let askOutput='';ask.stdout.on('data',b=>askOutput+=b);
 await until(async()=>assert.ok(lines.some(x=>x.event==='created'&&x.decision?.title==='テスト mini SSH ask')));
 const decision=lines.find(x=>x.event==='created'&&x.decision?.title==='テスト mini SSH ask');assert.equal(decision.machine.id,profiles[0].id);
 await run(['decision','answer',decision.decision.decision_id,'yes','--machine','mini-proof','--responder','cli-proof']);
 await until(async()=>assert.match(askOutput,/answered/));
 const duplicate=await run(['decision','answer',decision.decision.decision_id,'no','--machine','mini-proof'],true);assert.notEqual(duplicate.code,undefined);
 const final=JSON.parse(await ssh(['decision','get',decision.decision.decision_id]));assert.equal(final.result.decision.answer.option_id,'yes');
 await until(async()=>assert.ok(lines.some(x=>x.event==='resolved'&&x.decision?.decision_id===decision.decision.decision_id)));
 await run(['--machine','mini-proof','pane','report-agent',pane,'--source','live-ssh-test','--agent','codex','--state','idle']);
 const marker=remoteRoot+'/state/guard/agent-panes/p_'+pane.slice(1)+'.json';
 const guardWords=['env','GTX_PANE_ID='+pane,'GTX_GUARD_MARKER='+marker,'AI_GUARD_REQUEST_REASON=テスト SSH guard',remoteRoot+'/gtx','--session','proof','guard','exec','--','/bin/echo','deploy-proof-approve'];
 guard=spawn('ssh',[target,guardWords.map(quote).join(' ')]);guard.stdout.on('data',b=>guardOutput+=b);guard.stderr.on('data',b=>guardErr+=b);
 await until(async()=>assert.ok(lines.some(x=>x.event==='created'&&x.decision?.kind==='command_guard')));
 const guardDecision=lines.find(x=>x.event==='created'&&x.decision?.kind==='command_guard');
 assert.equal(guardOutput,'');await run(['decision','answer',guardDecision.decision.decision_id,'approve','--machine','mini-proof']);
 await until(async()=>assert.equal(guardOutput,'deploy-proof-approve\n'));
 const nativeBinary=process.env.GTX_LIVE_TEST_NATIVE_BINARY;
 if(nativeBinary) {
  const pluginRoot=path.resolve(path.dirname(new URL(import.meta.url).pathname),'../../apps/mac-dialog-decisions');
  nativeState=path.join(root,'state/plugins/kazuph.mac-dialog-decisions');
  const hash=createHash('sha256').update(fs.readFileSync(path.join(pluginRoot,'dialog.swift'))).digest('hex').slice(0,12);
  fs.mkdirSync(path.join(nativeState,'bin'),{recursive:true});
  fs.copyFileSync(nativeBinary,path.join(nativeState,'bin/gtx-decision-dialog-'+hash));
  fs.chmodSync(path.join(nativeState,'bin/gtx-decision-dialog-'+hash),0o755);
  await run(['workspace','rename','s1','テスト親Mac']);
  await run(['tab','rename','s1:t1','判断テスト']);
  await run(['pane','rename','p1','ローカル質問']);
  await ssh(['tab','rename',workspace.result.tab.tab_id,'mini判断テスト']);
  await run(['--machine','mini-proof','pane','rename',pane,'mini質問']);
  await run(['plugin','link',pluginRoot]);
  await run(['plugin','action','invoke','start','--plugin','kazuph.mac-dialog-decisions']);
  const screenshotReady=async(name,spec)=>{
   const destination=path.join(evidence,name+'.png');
   fs.writeFileSync(path.join(evidence,name+'-spec.json'),JSON.stringify(spec,null,2));
   const message=`隔離・音無効のMac実パネル表示中（${name}）。クリックせずネイティブパネルだけを撮影し ${destination} に保存してください。保存を検出したらCLIで回答して閉鎖を検証します。`;
   await exec(bin,['send','p4',message,'--from','machines'],{env:process.env});
   const check=await exec(bin,['pane','read','p4','--lines','15'],{env:process.env});
   if(check.stdout.includes(message)) await exec(bin,['pane','send-keys','p4','Enter'],{env:process.env});
   await new Promise((resolve,reject)=>{
    if(fs.existsSync(destination)) {resolve();return;}
    const timeout=setTimeout(()=>{observer.close();reject(Error('parent screenshot not delivered: '+destination));},180_000);
    const observer=fs.watch(evidence,()=>{if(fs.existsSync(destination)){clearTimeout(timeout);observer.close();resolve();}});
   });
  };
  for(const owner of process.env.GTX_LIVE_TEST_NATIVE_GUARD_ONLY==='1'?[]:['local','mini-proof']) {
   const title='テスト：'+(owner==='local'?'親Mac':'mini')+' ネイティブパネル';
   const argv=['--session','proof','decision','ask','--title',title,'--body','音は無効。CLIで回答し、パネルをクリックせず閉鎖を確認する。','--option','yes:テストを確認:approve','--option','no:却下:reject'];
   const paneArgs=owner==='local'?[]:['--machine','mini-proof'];
   const sourcePane=owner==='local'?'p1':pane;
   const sourceBin=owner==='local'?bin:remoteRoot+'/gtx';
   await run([...paneArgs,'pane','send-text',sourcePane,[sourceBin,...argv].map(quote).join(' ')]);
   await run([...paneArgs,'pane','send-keys',sourcePane,'Enter']);
   let requestOutput='';
   let specPath,spec;
   await until(async()=>{
    const dir=path.join(nativeState,'dialogs');
    const files=fs.readdirSync(dir).filter(x=>x.endsWith('.json')&&!x.endsWith('.result.jsonl'));
    specPath=files.map(x=>path.join(dir,x)).find(x=>JSON.parse(fs.readFileSync(x,'utf8')).title===title);
    assert.ok(specPath);spec=JSON.parse(fs.readFileSync(specPath,'utf8'));
   });
   assert.match(spec.location,/判断テスト/);
   if(owner==='mini-proof')assert.equal(spec.machine,'mini-proof');
   await screenshotReady(owner==='local'?'native-local':'native-mini',spec);
   const record=lines.find(x=>x.event==='created'&&x.decision?.title===title);assert.ok(record);
   await run(['decision','answer',record.decision.decision_id,'yes','--machine',owner,'--responder','native-test-cli']);
   await until(async()=>{assert.ok(!fs.existsSync(specPath));requestOutput=await run([...paneArgs,'pane','read',sourcePane,'--lines','100']);assert.match(requestOutput,/answered/);});
   const duplicate=await run(['decision','answer',record.decision.decision_id,'no','--machine',owner],true);
   assert.notEqual(duplicate.code,undefined);
   transcript.push({nativePanel:owner,decision:record.decision.decision_id,requestOutput,spec});
  }
  if(process.env.GTX_LIVE_TEST_NATIVE_GUARD_ONLY==='1') {
   const nativeGuard=spawn('ssh',[target,`cd /tmp && ${['env','GTX_PANE_ID='+pane,'GTX_GUARD_MARKER='+remoteRoot+'/state/guard/agent-panes/p_'+pane.slice(1)+'.json','AI_GUARD_REQUEST_REASON=テスト mini guard パネル',remoteRoot+'/gtx','--session','proof','guard','exec','--','/bin/echo','deploy-native-proof'].map(quote).join(' ')}`]);
   nativeRequests.push(nativeGuard);let output='',error='';nativeGuard.stdout.on('data',b=>output+=b);nativeGuard.stderr.on('data',b=>error+=b);
   let specPath,spec;
   await until(async()=>{
    const dir=path.join(nativeState,'dialogs');
    specPath=fs.readdirSync(dir).filter(x=>x.endsWith('.json')).map(x=>path.join(dir,x)).find(x=>JSON.parse(fs.readFileSync(x,'utf8')).title.includes('deploy-native-proof'));
    assert.ok(specPath);spec=JSON.parse(fs.readFileSync(specPath,'utf8'));
   });
   assert.equal(spec.machine,'mini-proof');assert.match(spec.location,/テスト mini.*mini質問/);assert.equal(spec.options.length,3);assert.equal(output,'');
   await screenshotReady('native-guard',spec);
   const record=lines.find(x=>x.event==='created'&&x.decision?.origin?.command?.includes('deploy-native-proof'));assert.ok(record);
   await run(['decision','answer',record.decision.decision_id,'approve','--machine','mini-proof','--responder','native-test-cli']);
   await until(async()=>{assert.ok(!fs.existsSync(specPath));assert.equal(output,'deploy-native-proof\n');});
   const duplicate=await run(['decision','answer',record.decision.decision_id,'approve','--machine','mini-proof'],true);assert.notEqual(duplicate.code,undefined);
   transcript.push({nativeGuard:{decision:record.decision.decision_id,output,error,spec}});
  }
  const pluginLogs=await run(['plugin','log','list','--plugin','kazuph.mac-dialog-decisions']);
  fs.writeFileSync(path.join(evidence,'plugin-events.json'),pluginLogs);
 }
 const jobLog=await run(['log',job.job]);assert.match(jobLog,/REMOTE_RUN_OK/);assert.match(jobLog,/exit_code: 0/);
 fs.writeFileSync(path.join(evidence,'watch.json'),JSON.stringify(lines,null,2));
 console.log(JSON.stringify({savedMachine:profiles[0],pane,remoteRun:job.job,ask:decision.decision.decision_id,guard:guardDecision.decision.decision_id,guardOutput,guardErr}));
});
