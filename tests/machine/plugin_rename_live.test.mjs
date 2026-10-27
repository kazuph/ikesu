import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import net from 'node:net';
import {createHash} from 'node:crypto';
import {spawn,execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {once} from 'node:events';
const exec=promisify(execFile);
const until=async fn=>{const deadline=Date.now()+15000;let error;do{try{return await fn();}catch(e){error=e;}await new Promise(r=>setTimeout(r,25));}while(Date.now()<deadline);throw error;};

test('正規の新名プラグインを登録しイベント起動・実パネル・CLI回答後の閉鎖を確認',async t=>{
 const {IKESU_PLUGIN_PROOF_BIN:bin,IKESU_PLUGIN_PROOF_NATIVE:native,IKESU_PLUGIN_PROOF_DIR:evidence,IKESU_PLUGIN_PROOF_REPORT_BIN:reportBin,IKESU_PLUGIN_PROOF_PARENT:parent,IKESU_PLUGIN_PROOF_CALLER:caller}=process.env;
 assert.ok(bin&&native&&evidence&&reportBin&&parent&&caller,'explicit proof binary/native/evidence/report route required');
 const reportEnv={...process.env,IKESU_PANE_ID:caller};
 const callerInfo=JSON.parse((await exec(reportBin,['pane','get',caller],{env:reportEnv})).stdout);
 assert.equal(callerInfo.result.pane.pane_id,caller);
 const parentInfo=JSON.parse((await exec(reportBin,['pane','get',parent],{env:reportEnv})).stdout);
 assert.equal(parentInfo.result.pane.pane_id,parent);
 const root=fs.mkdtempSync('/tmp/ip-'),home=root+'/home';fs.mkdirSync(home);
 fs.mkdirSync(evidence,{recursive:true});
 const env={...process.env};for(const key of Object.keys(env))if(/^(IKESU|MAC_DIALOG)_/.test(key))delete env[key];
 Object.assign(env,{HOME:home,SHELL:'/bin/sh',IKESU_STATE_DIR:root+'/state',IKESU_CONFIG:root+'/config.toml',IKESU_DISABLE_SOUND:'1'});
 fs.writeFileSync(env.IKESU_CONFIG,'onboarding=false\n[experimental]\nallow_nested=true\n[ui.sound]\nenabled=false\n[session]\nresume_agents_on_restore=false\n');
 const plugin=path.resolve('apps/mac-dialog-decisions'),state=env.IKESU_STATE_DIR+'/plugins/kazuph.mac-dialog-decisions';
 const hash=createHash('sha256').update(fs.readFileSync(plugin+'/dialog.swift')).digest('hex').slice(0,12);
 fs.mkdirSync(state+'/bin',{recursive:true});fs.copyFileSync(native,state+'/bin/ikesu-decision-dialog-'+hash);fs.chmodSync(state+'/bin/ikesu-decision-dialog-'+hash,0o700);
 const serverLog=fs.openSync(evidence+'/server.log','w');
 const server=spawn(bin,['--session','proof','server'],{env,stdio:['ignore',serverLog,serverLog]});
 const cli=async args=>(await exec(bin,['--session','proof',...args],{env,timeout:15000})).stdout;
 t.after(async()=>{
  try{const owner=JSON.parse(fs.readFileSync(state+'/daemon.json','utf8'));const command=(await exec('ps',['-o','command=','-p',String(owner.pid)])).stdout;if(command.includes(plugin+'/mac-dialog-decisions.mjs'))process.kill(owner.pid,'SIGTERM');}catch{}
  try{await cli(['server','stop']);}catch{}
  if(server.exitCode===null&&server.signalCode===null){server.kill('SIGTERM');await once(server,'close');}
  fs.closeSync(serverLog);
 });
 await until(()=>cli(['workspace','list']));
 await cli(['plugin','link',plugin]);
 const socket=root+'/state/proof/ikesu.sock';
 const rpc=(method,params)=>new Promise((resolve,reject)=>{const c=net.createConnection(socket);let raw='';c.on('error',reject);c.on('connect',()=>c.write(JSON.stringify({id:'plugin-proof',method,params})+'\n'));c.on('data',b=>{raw+=b;if(raw.includes('\n')){c.end();const response=JSON.parse(raw.split('\n')[0]);response.error?reject(Error(JSON.stringify(response.error))):resolve(response.result);}});});
 const created=await rpc('decision.create',{kind:'ask',title:'いけす新名プラグインの隔離実測',body:'正規のプラグイン登録から表示した判断です。撮影後、CLI回答で閉じます。クリック操作は行いません。',options:[{id:'confirmed',label:'CLIで確認済みにする',role:'approve'}]});
 const decision=created.decision.decision_id;
 const spec=await until(()=>{const dir=state+'/dialogs';const found=fs.readdirSync(dir).filter(f=>f.endsWith('.json')).map(f=>dir+'/'+f).find(f=>JSON.parse(fs.readFileSync(f)).decision_id===decision);assert.ok(found,'decision spec not yet written');return found;});
 const result=JSON.parse(fs.readFileSync(spec)).result_path;
 const nativePid=await until(async()=>{const all=(await exec('ps',['-axo','pid=,command='])).stdout.split('\n');const row=all.find(line=>line.includes(state+'/bin/ikesu-decision-dialog-'+hash)&&line.includes(spec));assert.ok(row,'native panel process not yet alive');return Number(row.trim().split(/\s+/,1)[0]);});
 const proof={root,plugin,manifest:plugin+'/ikesu-plugin.toml',decision,spec,result,nativePid,before:{spec:fs.existsSync(spec),result:fs.existsSync(result)},nativeSHA:createHash('sha256').update(fs.readFileSync(native)).digest('hex')};
 fs.writeFileSync(evidence+'/panel-ready.json',JSON.stringify(proof,null,2));
 console.log('PANEL_READY '+evidence+'/panel-ready.json');
 // The parent photographs the actual panel before allowing the CLI answer.
 const signal=evidence+'/continue-after-photo';
 if(!fs.existsSync(signal))await new Promise(resolve=>{const watcher=fs.watch(evidence,()=>{if(fs.existsSync(signal)){watcher.close();resolve();}});if(fs.existsSync(signal)){watcher.close();resolve();}});
 await cli(['decision','answer',decision,'confirmed','--responder','stage3-cli-proof']);
 await until(()=>{assert.equal(fs.existsSync(spec),false);assert.equal(fs.existsSync(result),false);assert.throws(()=>process.kill(nativePid,0));});
 const got=await rpc('decision.get',{decision_id:decision});assert.equal(got.decision.status,'answered');assert.equal(got.decision.answer.responder,'stage3-cli-proof');
 Object.assign(proof,{after:{spec:fs.existsSync(spec),result:fs.existsSync(result),nativeAlive:false},answer:got.decision.answer});
 fs.writeFileSync(evidence+'/proof.json',JSON.stringify(proof,null,2));
 console.log('PASS normal plugin link -> decision event -> ensure -> watch -> native panel -> CLI answer -> native exit and both paths absent');
});
