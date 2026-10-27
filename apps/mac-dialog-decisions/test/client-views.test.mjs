import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import net from 'node:net';
import {spawn} from 'node:child_process';
import {ClientViews} from '../client-views.mjs';
import {MacDialogDecisionsDaemon} from '../daemon.mjs';
import {createGetDecision} from '../mac-dialog-decisions.mjs';

async function until(predicate) {
  const end=Date.now()+10000;
  while(!await predicate()) { if(Date.now()>end) throw Error('real server deadline'); await new Promise(r=>setTimeout(r,5)); }
}
async function fixture(t) {
  const binary=process.env.MAC_DIALOG_TEST_IKESU; assert.ok(binary,'actual ikesu required');
  const root=fs.mkdtempSync('/tmp/dialog-view-'),dir=path.join(root,'isolated'); fs.mkdirSync(dir);
  const config=path.join(root,'config.toml');fs.writeFileSync(config,'[experimental]\nallow_nested=true\n');
  const env={...process.env,HOME:root,IKESU_CONFIG:config,IKESU_STATE_DIR:root,IKESU_SOCKET_PATH:path.join(dir,'ikesu.sock'),IKESU_SOCKET_PATH_EXPLICIT:'1',SHELL:'/bin/sh'};
  const server=spawn(binary,['--session','isolated','server'],{env,stdio:['ignore','ignore','pipe']});
  const exited=new Promise(resolve=>server.once('close',resolve));let stderr='';server.stderr.on('data',b=>stderr+=b);
  const sockets=[];
  const rpc=(method,params={})=>new Promise((resolve,reject)=>{
    const socket=net.createConnection(env.IKESU_SOCKET_PATH);let data='';
    socket.on('error',reject);socket.on('connect',()=>socket.write(JSON.stringify({id:'test',method,params})+'\n'));
    socket.on('data',b=>{data+=b;if(!data.includes('\n'))return;socket.end();const r=JSON.parse(data.split('\n')[0]);r.error?reject(Error(JSON.stringify(r.error))):resolve(r.result);});
  });
  await until(()=>{if(server.exitCode!==null)throw Error(stderr);return fs.existsSync(env.IKESU_SOCKET_PATH)});
  let changes=0;const views=new ClientViews(env);views.start(()=>changes++);await until(()=>views.ready);
  fs.writeFileSync(path.join(dir,'machines.json'),JSON.stringify([{id:'mini',session:'remote',enabled:true}]));
  const client=net.createConnection(path.join(dir,'ikesu-client.sock'));sockets.push(client);let incoming='';client.on('data',b=>incoming+=b);
  await new Promise((resolve,reject)=>{client.once('connect',resolve);client.once('error',reject)});
  client.write(JSON.stringify({type:'hello',protocol:1,generation:4,min_generation:1,cols:80,rows:24})+'\n');
  await until(()=>incoming.includes('welcome'));
  const send=view=>client.write(JSON.stringify({type:'client_view',client_view:view})+'\n');
  const set=async view=>{const before=changes;send(view);await until(()=>changes>before);return views.list();};
  const daemon=new MacDialogDecisionsDaemon({config:{stateDir:root,presentDelayMs:60000},clientViews:views,log:()=>{}});
  const entry=(id='dec-view',machineId='local',origin={pane_id:'p1'})=>{
    daemon.onCreated(machineId,{id:machineId},{decision_id:id,title:'view',options:[],origin});
    clearTimeout(daemon._pumpTimer);return [...daemon.entries.values()].find(e=>e.decision.decision_id===id);
  };
  t.after(async()=>{await daemon.stop();for(const s of sockets)s.destroy();try{await rpc('server.stop')}catch{}await exited;fs.rmSync(root,{recursive:true,force:true});});
  return {daemon,views,set,entry,rpc,client,env,get changes(){return changes}};
}
const local={machine_id:'local',session:'isolated',workspace_id:'s1',pane_ids:['p1','p2'],focused:true};

test('real API: focused same workspace suppresses, unknown/other space/session/machine/origin do not',async t=>{
 const f=await fixture(t),e=f.entry();
 for(const [patch,want] of [[{},true],[{focused:null},false],[{focused:false},false],[{workspace_id:'s2',pane_ids:['p3']},false],[{session:'other'},false],[{machine_id:'unknown'},false]]) {
   await f.set({...local,...patch});assert.equal(await f.daemon.presentation(e),want,JSON.stringify(patch));
 }
 await f.set(local);assert.equal(await f.daemon.presentation(f.entry('no-origin','local',null)),false);
 assert.equal(await f.daemon.presentation(f.entry('no-pane','local',{})),false);
 assert.equal(await f.daemon.presentation(f.entry('same-space-other-tab','local',{pane_id:'p2'})),true);
});

test('real API: saved remote ID/session and updated pane membership determine presentation',async t=>{
 const f=await fixture(t),e=f.entry('remote','mini');
 await f.set({...local,machine_id:'mini',session:'remote'});assert.equal(await f.daemon.presentation(e),true);
 await f.set({...local,machine_id:'mini',session:'other'});assert.equal(await f.daemon.presentation(e),false);
 await f.set({...local,machine_id:'mini',session:'remote',pane_ids:['p2']});assert.equal(await f.daemon.presentation(e),false);
 await f.set(local);assert.equal(await f.daemon.presentation(e),false);
});

test('real change notifications: leave emits front once; returning, leaving again and redisplay cannot emit twice',async t=>{
 const f=await fixture(t),e=f.entry();let current=await f.set(local);
 assert.equal(f.daemon.takeFrontCommand(e,true,current),null);
 current=await f.set({...local,workspace_id:'s2',pane_ids:['p3']});assert.deepEqual(f.daemon.takeFrontCommand(e,true,current),{status:'front'});
 current=await f.set(local);assert.equal(f.daemon.takeFrontCommand(e,true,current),null);
 current=await f.set({...local,machine_id:'mini',session:'remote'});assert.equal(f.daemon.takeFrontCommand(e,true,current),null);
 assert.equal(f.daemon.takeFrontCommand(e,false,[]),null);
});

test('real query: resolving/cancelling/disconnecting while awaiting response never presents',async t=>{
 const f=await fixture(t);await f.set(local);
 f.daemon.specFilePath=()=>path.join(f.views.directory,'cancelled-spec.json');
 for(const status of ['answered','cancelled','disconnect']) {
   const e=f.entry(status),pending=f.daemon._present(e);
   if(status==='disconnect') f.daemon.onMachineDown('local','test disconnect'); else f.daemon.onResolved('local',{decision_id:e.decision.decision_id,status});
   await pending;assert.equal(fs.existsSync(path.join(f.views.directory,'cancelled-spec.json')),false,status);
   assert.equal(e.showing,false);
 }
});

test('real query: repeated presentation rechecks, query failure falls back foreground, disconnect removes view',async t=>{
 const f=await fixture(t),e=f.entry();await f.set(local);assert.equal(await f.daemon.presentation(e),true);
 await f.set({...local,pane_ids:['p3']});assert.equal(await f.daemon.presentation(e),false);
 await f.set(local);assert.equal(await f.daemon.presentation(e),true);
 const before=f.changes;f.client.end();await until(()=>f.changes>before);
 assert.deepEqual(await f.views.list(),[]);assert.equal(await f.daemon.presentation(e),false);
 f.views.stop();assert.equal(await f.daemon.presentation(e),false);
});

test('query from an old connection epoch cannot present after reconnect',async t=>{
 const f=await fixture(t),e=f.entry();await f.set(local);
 const pending=f.daemon.presentation(e);
 f.daemon.machineEpochs.set('local',1);
 assert.equal(await pending,null);
 assert.equal(await f.daemon.presentation(e),true);
});

test('actual terminal client reports unknown then focus/blur and paints workspace switches',async t=>{
 const f=await fixture(t);
 const python=`import os,pty,select,sys,fcntl,termios,struct
pid,fd=pty.fork()
if pid==0:
 fcntl.ioctl(0,termios.TIOCSWINSZ,struct.pack("HHHH",24,80,0,0))
 os.execv(sys.argv[1],[sys.argv[1],"--session","isolated"])
while True:
 ready,_,_=select.select([fd,sys.stdin],[],[],1)
 if fd in ready:
  try: data=os.read(fd,65536)
  except OSError: break
  if not data: break
  if b"\\x1b[5n" in data: os.write(fd,b"\\x1b[0n")
 if sys.stdin in ready:
  data=os.read(sys.stdin.fileno(),4096)
  if data: os.write(fd,data)
 status=os.waitpid(pid,os.WNOHANG)
 if status[0]: break
os.close(fd)
`;
 const terminal=spawn('python3',['-c',python,process.env.MAC_DIALOG_TEST_IKESU],{env:f.env,stdio:['pipe','ignore','pipe']});
 let stderr='';terminal.stderr.on('data',b=>stderr+=b);
 const exited=new Promise(resolve=>terminal.once('close',resolve));
 t.after(async()=>{try{await f.rpc('server.stop')}catch{}await exited;assert.equal(terminal.exitCode,0,stderr)});
 let actual;
 await until(async()=>{actual=(await f.views.list()).find(view=>view.session==='isolated'&&view.workspace_id==='s1');return !!actual});
 assert.equal(actual.focused,null);
 terminal.stdin.write('\x1b[I');
 await until(async()=>{actual=(await f.views.list()).find(view=>view.client_id===actual.client_id);return actual?.focused===true});
 const space=await f.rpc('workspace.create',{label:'actual-client-space',focus:true});
 await until(async()=>{actual=(await f.views.list()).find(view=>view.client_id===actual.client_id);return actual?.workspace_id===space.workspace.workspace_id});
 assert.deepEqual(actual.pane_ids,[space.root_pane.pane_id]);
 terminal.stdin.write('\x1b[O');
 await until(async()=>{actual=(await f.views.list()).find(view=>view.client_id===actual.client_id);return actual?.focused===false});
 assert.equal(actual.machine_id,'local');
});

test('later and reconnect retain decision identity but query current display again',async t=>{
 const f=await fixture(t);
 const {decision}=await f.rpc('decision.create',{kind:'ask',title:'redisplay',origin:{pane_id:'p1'},options:[]});
 f.daemon.onCreated('local',{id:'local'},decision);clearTimeout(f.daemon._pumpTimer);
 const e=[...f.daemon.entries.values()][0];
 await f.set(local);assert.equal(await f.daemon.presentation(e),true);
 f.daemon.config.laterMs=0;f.daemon.snooze(e);
 await f.set({...local,pane_ids:['p3'],workspace_id:'s2'});
 await until(()=>e.queued&&!e.dismissed);clearTimeout(f.daemon._pumpTimer);
 assert.equal(await f.daemon.presentation(e),false);
 e.queued=false;e.draft={text:'retained',option_ids:[]};
 f.daemon.getDecision=createGetDecision({ikesuBin:process.env.MAC_DIALOG_TEST_IKESU},()=>{},f.env);
 f.daemon.onMachineDown('local','test reconnect');
 await f.set(local);f.daemon.onWatchLine({event:'machine_up',machine:{id:'local'}});
 await until(()=>!e.machineDown&&e.queued);clearTimeout(f.daemon._pumpTimer);
 assert.equal(await f.daemon.presentation(e),true);assert.equal(e.draft.text,'retained');
});

test('any matching focused client suppresses; removing that client immediately changes the result',async t=>{
 const f=await fixture(t),e=f.entry();await f.set({...local,focused:null});
 const other=net.createConnection(path.join(f.views.directory,'ikesu-client.sock'));
 t.after(()=>other.destroy());let incoming='';other.on('data',b=>incoming+=b);
 await new Promise((resolve,reject)=>{other.once('connect',resolve);other.once('error',reject)});
 other.write(JSON.stringify({type:'hello',protocol:1,generation:4,min_generation:1,cols:80,rows:24})+'\n');
 await until(()=>incoming.includes('welcome'));
 const before=f.changes;other.write(JSON.stringify({type:'client_view',client_view:local})+'\n');await until(()=>f.changes>before);
 assert.equal((await f.views.list()).length,2);assert.equal(await f.daemon.presentation(e),true);
 const count=f.changes;other.end();await until(()=>f.changes>count);assert.equal(await f.daemon.presentation(e),false);
});
