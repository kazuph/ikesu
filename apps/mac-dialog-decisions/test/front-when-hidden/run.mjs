#!/usr/bin/env node
// Explicit manual acceptance driver. `self-check` never creates a window.
import fs from 'node:fs';
import path from 'node:path';
import net from 'node:net';
import {fileURLToPath} from 'node:url';
import {spawn,execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {ClientViews} from '../../client-views.mjs';
import {NativeDialogHost} from '../../native-host.mjs';
import {MacDialogDecisionsDaemon,spawnDecisionWatch,createGetDecision,createRunAnswer} from '../../mac-dialog-decisions.mjs';
const exec=promisify(execFile);
const repository=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../../../..');
const output=path.join(repository,'.artifacts/cross-machine-nav');
const mode=process.argv[2]||'help';
const root=process.argv[3];
const usage='run.mjs self-check | start <new-absolute-directory> | attach <directory> | prepare <directory> | focus <directory> <workspace-id> | create <directory> <pane-id> <a|b|c> | clear <directory> | record <directory> <label> | stop <directory>';
if(mode==='help'){console.log(usage);process.exit(0)}
const binary=path.join(output,'ikesu-task3'),native=path.join(output,'dialog-front-test');
function environment(directory) {return {...process.env,HOME:directory,IKESU_STATE_DIR:path.join(directory,'state'),IKESU_CONFIG:path.join(directory,'config.toml'),IKESU_SOCKET_PATH:path.join(directory,'state/dialog-front-task3/ikesu.sock'),IKESU_SOCKET_PATH_EXPLICIT:'1',SHELL:'/bin/sh',TASK3_NATIVE_TRACE:path.join(directory,'native.jsonl')};}
function rpc(socketPath,method,params={}) {return new Promise((resolve,reject)=>{
 const socket=net.createConnection(socketPath);let data='';
 socket.on('error',reject);socket.on('connect',()=>socket.write(JSON.stringify({id:'manual-test',method,params})+'\n'));
 socket.on('data',b=>{data+=b;if(!data.includes('\n'))return;socket.end();const response=JSON.parse(data.split('\n')[0]);response.error?reject(Error(JSON.stringify(response.error))):resolve(response.result);});
});}
async function until(predicate) {const end=Date.now()+10000;while(!await predicate()){if(Date.now()>end)throw Error('isolated runtime deadline');await new Promise(r=>setTimeout(r,10));}}
async function start(directory) {
 if(path.join(directory,'state/dialog-front-task3/ikesu-client.sock').length>100)throw Error('use a short path such as /tmp/ikesu-task3-review (Unix socket path limit)');
 if(!path.isAbsolute(directory)||fs.existsSync(directory))throw Error('start requires a new absolute directory; existing state is never overwritten');
 fs.mkdirSync(directory,{recursive:true});fs.writeFileSync(path.join(directory,'config.toml'),'[experimental]\nallow_nested=true\n');
 const env=environment(directory),log=message=>fs.appendFileSync(path.join(directory,'daemon.log'),message+'\n');
 const server=spawn(binary,['--session','dialog-front-task3','server'],{env,stdio:['ignore','ignore',fs.openSync(path.join(directory,'server.log'),'a')]});
 const serverExit=new Promise(resolve=>server.once('close',resolve));
 await until(()=>{if(server.exitCode!==null)throw Error('isolated server exited');return fs.existsSync(env.IKESU_SOCKET_PATH)});
 const specDir=path.join(directory,'dialogs');fs.mkdirSync(specDir);
 const host=new NativeDialogHost(native,specDir,log);
 // Test binary observes the actual DialogHost; production code has no inspect op.
 process.env.TASK3_NATIVE_TRACE=env.TASK3_NATIVE_TRACE;
 host.start();await until(()=>fs.existsSync(env.TASK3_NATIVE_TRACE));
 const config={ikesuBin:binary,stateDir:directory,presentDelayMs:0};
 const daemon=new MacDialogDecisionsDaemon({config,clientViews:new ClientViews(env),
  spawnWatch:()=>Promise.resolve(spawnDecisionWatch(config,log,env)),getDecision:createGetDecision(config,log,env),runAnswer:createRunAnswer(config,log,env),
  spawnDialog:file=>host.open(file),specFilePath:key=>path.join(specDir,encodeURIComponent(key)+'.json'),log});
 await daemon.start();
 let stopping=false;
 const control=net.createServer(socket=>{
  let raw='';socket.on('data',async b=>{raw+=b;if(!raw.includes('\n'))return;socket.pause();try{
   const request=JSON.parse(raw.split('\n')[0]);
   if(request.method==='record'){
     host.send({op:'inspect',label:request.params.label});
     await until(()=>fs.readFileSync(env.TASK3_NATIVE_TRACE,'utf8').trim().split('\n').map(JSON.parse).some(row=>row.label===request.params.label));
     const view=await rpc(env.IKESU_SOCKET_PATH,'client.view.list');
     fs.appendFileSync(path.join(directory,'views.jsonl'),JSON.stringify({label:request.params.label,...view})+'\n');
     socket.end(JSON.stringify({result:{type:'recorded',directory}})+'\n');
   } else if(request.method==='stop'){
     if(stopping)return;stopping=true;await daemon.stop();await host.stop();await rpc(env.IKESU_SOCKET_PATH,'server.stop');await serverExit;
     socket.end(JSON.stringify({result:{type:'stopped',server_exit:server.exitCode,host_exit:host.child?.exitCode??0}})+'\n');control.close();
   } else throw Error('unsupported control request');
  }catch(error){socket.end(JSON.stringify({error:{message:error.message}})+'\n')}});
 });
 await new Promise(resolve=>control.listen(path.join(directory,'control.sock'),resolve));
 fs.writeFileSync(path.join(directory,'runtime.json'),JSON.stringify({server_pid:server.pid,host_pid:host.child.pid,supervisor_pid:process.pid,session:'dialog-front-task3'},null,2));
 console.log(JSON.stringify({ready:true,directory}));
}
if(mode==='start'){await start(root)}
else if(mode==='self-check'){
 const directory=`/tmp/ikesu-task3-${process.pid}`;
 const supervisor=spawn(process.execPath,[fileURLToPath(import.meta.url),'start',directory],{stdio:['ignore','pipe','pipe']});let stderr='';supervisor.stderr.on('data',b=>stderr+=b);
 const done=new Promise(resolve=>supervisor.once('close',resolve));
 await until(()=>{if(supervisor.exitCode!==null)throw Error(stderr);return fs.existsSync(path.join(directory,'control.sock'))});
 const record=await rpc(path.join(directory,'control.sock'),'record',{label:'headless-start-stop'});
 const stop=await rpc(path.join(directory,'control.sock'),'stop');await done;
 const rows=fs.readFileSync(path.join(directory,'native.jsonl'),'utf8').trim().split('\n').map(JSON.parse);
 if(rows.some(row=>row.window_count!==0)||stop.server_exit!==0||supervisor.exitCode!==0)throw Error('headless runtime check failed');
 console.log(JSON.stringify({pass:true,record,stop,rows:rows.map(({screen_windows,...row})=>row),directory}));
}else {
 if(!root||!path.isAbsolute(root))throw Error(usage);
 const env=environment(root);
 if(mode==='attach'){
  const client=spawn(binary,['--session','dialog-front-task3'],{env,stdio:'inherit'});client.on('close',code=>process.exit(code??1));
 }else if(mode==='prepare'){
  const list=await rpc(env.IKESU_SOCKET_PATH,'workspace.list');
  const target=list.workspaces[0];if(!target)throw Error('attach the isolated terminal first');
  await rpc(env.IKESU_SOCKET_PATH,'workspace.rename',{workspace_id:target.workspace_id,label:'task3-target'});
  const away=await rpc(env.IKESU_SOCKET_PATH,'workspace.create',{label:'task3-away',focus:false});
  await rpc(env.IKESU_SOCKET_PATH,'workspace.focus',{workspace_id:target.workspace_id});
  const panes=await rpc(env.IKESU_SOCKET_PATH,'pane.list');
  console.log(JSON.stringify({target:target.workspace_id,away:away.workspace.workspace_id,panes},null,2));
 }else if(mode==='focus'){
  console.log(JSON.stringify(await rpc(env.IKESU_SOCKET_PATH,'workspace.focus',{workspace_id:process.argv[4]})));
 }else if(mode==='clear'){
  const list=await rpc(env.IKESU_SOCKET_PATH,'decision.list',{status:'pending'});
  for(const decision of list.decisions)await rpc(env.IKESU_SOCKET_PATH,'decision.cancel',{decision_id:decision.decision_id});
  console.log(JSON.stringify({cancelled:list.decisions.length}));
 }else if(mode==='create'){
  const [pane,label]=process.argv.slice(4);if(!/^p\d+$/.test(pane)||!['a','b','c'].includes(label))throw Error(usage);
  const result=await rpc(env.IKESU_SOCKET_PATH,'decision.create',{kind:'ask',title:`task3 ${label}: panel visibility`,origin:{pane_id:pane},allow_text:true,options:[{id:'yes',label:'確認済み',role:'approve'}]});
  fs.appendFileSync(path.join(root,'decisions.jsonl'),JSON.stringify({label,...result})+'\n');console.log(JSON.stringify(result));
 }else if(mode==='record'){
  const label=process.argv[4];if(!/^[a-zA-Z0-9_-]+$/.test(label??''))throw Error('record requires a simple label');
  console.log(JSON.stringify(await rpc(path.join(root,'control.sock'),'record',{label})));
  await exec('/usr/sbin/screencapture',['-x',path.join(root,label+'.png')]);
  const row=fs.readFileSync(path.join(root,'native.jsonl'),'utf8').trim().split('\n').map(JSON.parse).findLast(row=>row.label===label);
  const screen=row.screen_windows.filter(w=>w.layer===0);
  const inputIndex=screen.findIndex(w=>w.pid===row.frontmost_pid);
  const panels=row.panels.map(panel=>({...panel,screen_index:screen.findIndex(w=>w.number===panel.window_number)}));
  console.log(JSON.stringify({label,frontmost_pid:row.frontmost_pid,input_target:row.input_target,input_index:inputIndex,app_active:row.app_active,window_count:row.window_count,panels,screenshot:path.join(root,label+'.png')},null,2));
 }else if(mode==='stop'){console.log(JSON.stringify(await rpc(path.join(root,'control.sock'),'stop')))}
 else throw Error(usage);
}
