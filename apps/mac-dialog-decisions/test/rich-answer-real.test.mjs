import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import net from 'node:net';
import {spawn} from 'node:child_process';
import {MacDialogDecisionsDaemon,createRunAnswer,spawnDecisionWatch} from '../mac-dialog-decisions.mjs';
import {RichService} from '../rich-service.mjs';

async function fixture(t){
 const binary=process.env.MAC_DIALOG_TEST_IKESU,nativeDialog=process.env.MAC_DIALOG_TEST_NATIVE_BINARY;
 assert.ok(binary&&nativeDialog,'actual binaries required');
 const root=await fs.mkdtemp('/tmp/dialog-rich-real-');
 const configFile=path.join(root,'config.toml');await fs.writeFile(configFile,'[experimental]\nallow_nested=true\n');
 const socket=path.join(root,'state','default','ikesu.sock');
 const env={...process.env,IKESU_CONFIG:configFile,IKESU_STATE_DIR:path.join(root,'state'),IKESU_SOCKET_PATH:socket,IKESU_SOCKET_PATH_EXPLICIT:'1',SHELL:'/bin/sh'};
 const server=spawn(binary,['server'],{env,stdio:['ignore','ignore','pipe']});const exited=new Promise(resolve=>server.once('close',resolve));
 let stderr='';server.stderr.on('data',data=>stderr+=data);
 const rpc=(method,params={})=>new Promise((resolve,reject)=>{const client=net.createConnection(socket);let data='';client.on('connect',()=>client.write(JSON.stringify({id:'rich-real',method,params})+'\n'));client.on('error',reject);client.on('data',chunk=>{data+=chunk;if(!data.includes('\n'))return;client.end();const reply=JSON.parse(data.split('\n')[0]);reply.error?reject(Error(JSON.stringify(reply.error))):resolve(reply.result);});});
 const end=Date.now()+10000;while(true){try{await fs.stat(socket);break;}catch{}if(Date.now()>end||server.exitCode!==null)throw Error(stderr||'socket deadline');await new Promise(resolve=>setTimeout(resolve,10));}
 const config={ikesuBin:binary,nativeDialog,stateDir:path.join(root,'panel'),presentDelayMs:60000};
 const rich=new RichService(config);
 const daemon=new MacDialogDecisionsDaemon({config,rich,runAnswer:createRunAnswer(config,()=>{},env),log:()=>{}});
 t.after(async()=>{await daemon.stop();try{await rpc('server.stop')}catch{}await exited;await fs.rm(root,{recursive:true,force:true});});
 return {root,rpc,rich,daemon,env,config};
}
const png=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6ZAAAAABJRU5ErkJggg==','base64');
test('real CLI saves image-only answer before answering and keeps its path readable',async t=>{
 const f=await fixture(t);const decision=(await f.rpc('decision.create',{kind:'ask',title:'image-only',allow_text:true,options:[{id:'yes',label:'はい',role:'approve'}]})).decision;
 f.daemon.onCreated('local',{id:'local'},decision);clearTimeout(f.daemon._pumpTimer);
 const dir=await f.rich.store.draftDirectory('local',decision.decision_id);const source=path.join(dir,'日本語 空白.png');await fs.writeFile(source,png);
 const entry=[...f.daemon.entries.values()][0];assert.equal(await f.daemon.submitAnswer(entry,{attachments:[{path:source,name:'日本語 空白.png'}]}),true);
 const saved=(await f.rpc('decision.get',{decision_id:decision.decision_id})).decision;
 assert.equal(saved.status,'answered');assert.match(saved.answer.text,/^添付画像: \/.*\/attachments\/local\//);
 const file=saved.answer.text.slice('添付画像: '.length);assert.deepEqual(await fs.readFile(file),png);assert.equal((await fs.stat(file)).mode&0o777,0o600);
});
test('actual save failure sends no answer and keeps text plus attachments in draft',async t=>{
 const f=await fixture(t);const decision=(await f.rpc('decision.create',{kind:'ask',title:'save failure',allow_text:true,options:[{id:'yes',label:'はい',role:'approve'}]})).decision;
 f.daemon.onCreated('local',{id:'local'},decision);clearTimeout(f.daemon._pumpTimer);
 const dir=await f.rich.store.draftDirectory('local',decision.decision_id);const source=path.join(dir,'link.png');const outside=path.join(f.root,'outside.png');await fs.writeFile(outside,png);await fs.symlink(outside,source);
 const entry=[...f.daemon.entries.values()][0];const attachments=[{path:source,name:'link.png'}];
 assert.equal(await f.daemon.submitAnswer(entry,{text:'残す',attachments}),false);
 assert.equal((await f.rpc('decision.get',{decision_id:decision.decision_id})).decision.status,'pending');assert.deepEqual(entry.draft,{option_ids:[],text:'残す',attachments});
 clearTimeout(f.daemon._pumpTimer);
});
test('actual cancellation during attachment preparation cannot answer or leave cancelled images',async t=>{
 const f=await fixture(t);const decision=(await f.rpc('decision.create',{kind:'ask',title:'cancel upload',allow_text:true,options:[{id:'yes',label:'はい',role:'approve'}]})).decision;
 f.daemon.onCreated('local',{id:'local'},decision);clearTimeout(f.daemon._pumpTimer);
 const dir=await f.rich.store.draftDirectory('local',decision.decision_id);const source=path.join(dir,'1.png');await fs.writeFile(source,png);
 const entry=[...f.daemon.entries.values()][0];const sending=f.daemon.submitAnswer(entry,{attachments:[{path:source,name:'1.png'}]});
 const cancelled=(await f.rpc('decision.cancel',{decision_id:decision.decision_id})).decision;f.daemon.onResolved('local',cancelled);
 assert.equal(await sending,false);assert.equal((await f.rpc('decision.get',{decision_id:decision.decision_id})).decision.status,'cancelled');
 await assert.rejects(fs.stat(path.join(f.rich.config.stateDir,'attachments','local',decision.decision_id)),{code:'ENOENT'});
});


test('daemon restart keeps incomplete journals visible while watching and healthy cleanup continue',async t=>{
 const f=await fixture(t),issues=[];f.rich.store.log=message=>issues.push(message);
 for(const [id,data] of [['directory',null],['empty',''],['partial','{"status":']]) {
  const dir=path.join(f.config.stateDir,'resolutions/local',id);await fs.mkdir(dir,{recursive:true});if(data!==null)await fs.writeFile(path.join(dir,'resolution.json'),data);
 }
 const {execFile}=await import('node:child_process'),{promisify}=await import('node:util');
 const writer=path.join(f.root,'interrupted-write.mjs');
 await fs.writeFile(writer,`import {AttachmentStore} from ${JSON.stringify(new URL('../attachments.mjs',import.meta.url).href)};await new AttachmentStore(process.argv[2]).queueResolution('local','interrupted','cancelled',1000);`);
 await assert.rejects(promisify(execFile)('/bin/sh',['-c','ulimit -f 0; exec "$@"','journal-write',process.execPath,writer,f.config.stateDir]));
 const healthy=await f.rich.store.directory('attachments','local','healthy');await fs.writeFile(path.join(healthy,'1.png'),png);
 await f.rich.store.queueResolution('local','healthy','cancelled',1000);
 f.daemon.spawnWatch=()=>spawnDecisionWatch(f.config,()=>{},f.env);
 await f.daemon.start();
 await assert.rejects(fs.stat(healthy),{code:'ENOENT'});
 const decision=(await f.rpc('decision.create',{kind:'ask',title:'watch after incomplete journal',allow_text:true,options:[{id:'yes',label:'はい',role:'approve'}]})).decision;
 const deadline=Date.now()+10000;
 while(![...f.daemon.entries.values()].some(e=>e.decision.decision_id===decision.decision_id)) {
  assert.ok(Date.now()<deadline,'actual watch must deliver the unrelated decision');await new Promise(resolve=>setTimeout(resolve,10));
 }
 const badIds=['directory','empty','partial','interrupted'];
 for(const id of badIds)assert.ok(issues.some(message=>message.includes('/'+id+'/')),id);
 assert.ok(issues.every(message=>badIds.some(id=>message.includes('/'+id+'/'))),'only the retained incomplete records are reported');
 assert.equal(await fs.readFile(path.join(f.config.stateDir,'resolutions/local/empty/resolution.json'),'utf8'),'');
 assert.equal(await fs.readFile(path.join(f.config.stateDir,'resolutions/local/partial/resolution.json'),'utf8'),'{"status":');
 assert.ok((await fs.readdir(path.join(f.config.stateDir,'resolutions/local/interrupted'))).some(name=>name.endsWith('.writing')));
});
