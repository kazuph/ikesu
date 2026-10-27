// Launch a disposable Chrome profile owned by this test. Never attaches to a
// user's browser. Disable Playwright's synthetic focus to observe native visibility.
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
import {resolve,join} from 'node:path';
import {readFile,writeFile,mkdtemp,rm} from 'node:fs/promises';
import {watch} from 'node:fs';
import {tmpdir} from 'node:os';
import {spawn,execFileSync} from 'node:child_process';

const profile=await mkdtemp(join(tmpdir(),'koi-visibility-p232-'));
const activeFile=join(profile,'DevToolsActivePort');
const endpoint=new Promise((resolve,reject)=>{const timeout=setTimeout(()=>{watcher.close();reject(Error('owned Chrome did not start'));},15000);const watcher=watch(profile,async(_event,name)=>{if(name!=='DevToolsActivePort')return;try{const text=await readFile(activeFile,'utf8');const [port]=text.trim().split('\n');if(!/^\d+$/.test(port))return;clearTimeout(timeout);watcher.close();resolve('http://127.0.0.1:'+port);}catch{}});});
const chrome=spawn('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--user-data-dir='+profile,'--remote-debugging-port=0','--no-first-run','--no-default-browser-check','about:blank'],{stdio:'ignore'});
let socket;
try {
 const info=await (await fetch((await endpoint)+'/json/version')).json();
 socket=new WebSocket(info.webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{socket.addEventListener('open',resolve,{once:true});socket.addEventListener('error',reject,{once:true});});
 let serial=0;const pending=new Map();
 socket.addEventListener('message',event=>{const reply=JSON.parse(event.data);const waiter=pending.get(reply.id);if(waiter){pending.delete(reply.id);if(reply.error)waiter.reject(Error(JSON.stringify(reply.error)));else waiter.resolve(reply.result);}});
 const call=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++serial;pending.set(id,{resolve,reject});socket.send(JSON.stringify({id,method,params,sessionId}));});
 const pond=await call('Target.createTarget',{url:pathToFileURL(resolve('site/index.html')).href});
 const {sessionId}=await call('Target.attachToTarget',{targetId:pond.targetId,flatten:true});
 const evaluate=async expression=>{const reply=await call('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true},sessionId);if(reply.exceptionDetails)throw Error(JSON.stringify(reply.exceptionDetails));return reply.result.value;};
 await call('Target.activateTarget',{targetId:pond.targetId});
 await evaluate("new Promise((resolve,reject)=>{const end=Date.now()+5000;function check(){if(typeof clock!=='undefined'&&clock>0)resolve(true);else if(Date.now()>end)reject(Error('pond did not start'));else setTimeout(check,50)}check()})");
 const other=await call('Target.createTarget',{url:'about:blank'});
 await call('Target.activateTarget',{targetId:other.targetId});
 await evaluate("new Promise((resolve,reject)=>{const end=Date.now()+5000;function check(){if(document.hidden)resolve(true);else if(Date.now()>end)reject(Error('native tab did not hide'));else setTimeout(check,50)}check()})");
 const snapshot='({hidden:document.hidden,raf,clock,fish:JSON.stringify(fish)})';
 const before=await evaluate(snapshot);
 await evaluate('new Promise(resolve=>setTimeout(resolve,300))');
 const after=await evaluate(snapshot);assert.deepEqual(after,before);assert.equal(after.raf,0);
 await call('Target.activateTarget',{targetId:pond.targetId});
 await evaluate(`new Promise((resolve,reject)=>{const end=Date.now()+5000;function check(){if(!document.hidden&&clock>${before.clock})resolve(true);else if(Date.now()>end)reject(Error('pond did not resume'));else setTimeout(check,50)}check()})`);
 const result={before,after,resumed:await evaluate('clock'),ownedProfile:profile};
 await writeFile(join(resolve(process.argv[2]),'visibility-results.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));

}finally{
 if(socket)socket.close();
 const uid=execFileSync('id',['-u'],{encoding:'utf8'}).trim();
 let owned=[];try{owned=execFileSync('pgrep',['-u',uid,'-f',profile],{encoding:'utf8'}).trim().split(/\s+/).filter(Boolean);}catch{}
 for(const pid of owned){let command;try{command=execFileSync('ps',['-ww','-p',pid,'-o','command='],{encoding:'utf8'});}catch(error){if(error.status===1)continue;throw error;}if(command.includes(profile)&&command.includes('/Applications/Google Chrome.app/'))execFileSync('kill',[pid]);}
 await rm(profile,{recursive:true,force:true});
}
