import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import {spawn,execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {SlackDecisionsDaemon,SlackClient,REACTION_GRACE_MS} from '../slack-decisions.mjs';
import {FakeSlack,waitUntil} from './fakes.mjs';
import {TestClock} from './clock.mjs';
const exec=promisify(execFile);

async function fixture(t,{multi=false}={}){
 const binary=process.env.MAC_DIALOG_TEST_IKESU;assert.ok(binary,'actual ikesu required');
 const root=await fs.mkdtemp('/tmp/slack-grace-');const dir=path.join(root,'isolated');await fs.mkdir(dir);
 const config=path.join(root,'config.toml');await fs.writeFile(config,'onboarding=false\n[experimental]\nallow_nested=true\n[ui.sound]\nenabled=false\n');
 const env={...process.env,HOME:root,IKESU_CONFIG:config,IKESU_STATE_DIR:root,IKESU_SOCKET_PATH:path.join(dir,'ikesu.sock'),IKESU_SOCKET_PATH_EXPLICIT:'1',SHELL:'/bin/sh'};
 const server=spawn(binary,['--session','isolated','server'],{env,stdio:['ignore','ignore','pipe']});const exited=new Promise(resolve=>server.once('close',resolve));let stderr='';server.stderr.on('data',data=>stderr+=data);
 await waitUntil(async()=>{assert.equal(server.exitCode,null,stderr);return fs.stat(env.IKESU_SOCKET_PATH).then(()=>true,()=>false)});
 const rpc=(method,params={})=>new Promise((resolve,reject)=>{
  const socket=net.createConnection(env.IKESU_SOCKET_PATH);let data='';socket.on('error',reject);socket.on('connect',()=>socket.write(JSON.stringify({id:'grace-test',method,params})+'\n'));
  socket.on('data',bytes=>{data+=bytes;if(!data.includes('\n'))return;socket.end();const response=JSON.parse(data.split('\n')[0]);response.error?reject(Error(JSON.stringify(response.error))):resolve(response.result);});
 });
 const api=await new FakeSlack().start(),clock=new TestClock();
 const daemon=new SlackDecisionsDaemon({config:{stateDir:root,socketMode:false},slack:new SlackClient({token:'local-emulator',channel:'CLOCAL',apiBase:api.base}),botUserId:api.botId,clock,glassesState:async()=>null,log:()=>{},runAnswer:async args=>{try{const result=await exec(binary,args,{env,timeout:10000});return {code:0,...result}}catch(error){return {code:error.code||1,stderr:error.stderr}}}});
 t.after(async()=>{await daemon.stop();await api.stop();try{await rpc('server.stop')}catch{}await exited;await fs.rm(root,{recursive:true,force:true});});
 const decision=(await rpc('decision.create',{kind:'ask',title:'15秒の取り消し猶予',allow_text:true,multi_select:multi,options:[{id:'approve',label:'承認',role:'approve'},{id:'reject',label:'却下',role:'reject'}]})).decision;
 await daemon.onCreated('local',{id:'local'},decision);const entry=[...daemon.entries.values()][0];
 const saved=async()=>(await rpc('decision.get',{decision_id:decision.decision_id})).decision;
 const react=async(name,{removed=false,deliver=true}={})=>{
  if(removed)api.reactionsByTs.set(entry.post.ts,(api.reactionsByTs.get(entry.post.ts)||[]).filter(reaction=>reaction.name!==name));else api.userReact(entry.post.ts,name);
  if(deliver)await daemon.handleSocketPayload({type:removed?'reaction_removed':'reaction_added',user:'UHUMAN',reaction:name,event_ts:String(clock.now()/1000),item:{type:'message',ts:entry.post.ts}});
 };
 const reply=async(text,{deliver=true}={})=>{
  const message={ts:String(clock.now()/1000),user:'UHUMAN',text};const rows=api.threadReplies.get(entry.post.ts)||[];rows.push(message);api.threadReplies.set(entry.post.ts,rows);
  if(deliver)await daemon.handleSocketPayload({type:'message',thread_ts:entry.post.ts,...message});
 };
 return {clock,daemon,api,entry,decision,rpc,saved,react,reply};
}
const ids=decision=>decision.answer?.option_ids?.length?decision.answer.option_ids:[decision.answer?.option_id].filter(Boolean);

test('both events initially unprocessed: comment wins at the grace deadline through real Slack history and real saved decision',async t=>{
 const f=await fixture(t);await f.react('one',{deliver:false});await f.clock.advance(1);await f.reply('まだ削除しないで',{deliver:false});await f.daemon.pollOnce();
 assert.equal((await f.saved()).status,'pending');await f.clock.advance(REACTION_GRACE_MS-1);assert.equal((await f.saved()).status,'pending');await f.clock.advance(1);
 const saved=await f.saved();assert.equal(saved.answer.text,'まだ削除しないで');assert.deepEqual(ids(saved),[]);
});

test('reply during grace clears the option, and explicit reselection carries both and restarts the full fifteen seconds',async t=>{
 const f=await fixture(t);await f.react('one');await f.clock.advance(REACTION_GRACE_MS/3);await f.reply('説明を追加');
 assert.deepEqual(f.entry.reactionDraft.optionIds,[]);await f.clock.advance(REACTION_GRACE_MS/3);await f.react('one',{removed:true});await f.react('two');
 await f.clock.advance(REACTION_GRACE_MS-1);assert.equal((await f.saved()).status,'pending');await f.clock.advance(1);
 const saved=await f.saved();assert.deepEqual(ids(saved),['reject']);assert.equal(saved.answer.text,'説明を追加');
});

test('reply after grace cannot override a saved numbered answer',async t=>{
 const f=await fixture(t);await f.react('one');await f.clock.advance(REACTION_GRACE_MS);
 const before=await f.saved();assert.deepEqual(ids(before),['approve']);await f.clock.advance(1);await f.reply('確定後の返信');
 assert.deepEqual((await f.saved()).answer,before.answer);
});

test('a comment during grace without reselection saves only the comment',async t=>{
 const f=await fixture(t);await f.react('two');await f.clock.advance(REACTION_GRACE_MS/3);await f.reply('コメントだけ');await f.clock.advance(REACTION_GRACE_MS);
 assert.deepEqual(ids(await f.saved()),[]);assert.equal((await f.saved()).answer.text,'コメントだけ');
});

test('multiple choices retain explicit check confirmation and reset grace after the last number removal',async t=>{
 const f=await fixture(t,{multi:true});await f.react('one');await f.clock.advance(REACTION_GRACE_MS);assert.equal((await f.saved()).status,'pending','numbers alone never confirm multi-select');
 await f.react('two');await f.react('white_check_mark');await f.clock.advance(REACTION_GRACE_MS/3);await f.react('one',{removed:true});
 await f.clock.advance(REACTION_GRACE_MS-1);assert.equal((await f.saved()).status,'pending');await f.clock.advance(1);assert.deepEqual(ids(await f.saved()),['reject']);
});

test('multiple-choice comment wins during a pending confirmation; reselection plus check carries both',async t=>{
 const f=await fixture(t,{multi:true});await f.react('one');await f.react('white_check_mark');await f.clock.advance(REACTION_GRACE_MS/3);await f.reply('この条件で');
 assert.equal(f.entry.selection.size,0);await f.react('two');await f.react('white_check_mark',{removed:true});await f.react('white_check_mark');await f.clock.advance(REACTION_GRACE_MS);
 const saved=await f.saved();assert.deepEqual(ids(saved),['reject']);assert.equal(saved.answer.text,'この条件で');
});

test('cancellation inside grace cancels the timer and never sends a saved approval',async t=>{
 const f=await fixture(t);await f.react('one');const cancelled=(await f.rpc('decision.cancel',{decision_id:f.decision.decision_id})).decision;
 await f.daemon.onResolved('local',cancelled);await f.clock.advance(REACTION_GRACE_MS);assert.equal((await f.saved()).status,'cancelled');assert.equal(f.clock.timers.size,0);
});


test('undelivered number removal at fourteen seconds is reconciled before expiry; no approval is saved',async t=>{
 const f=await fixture(t);await f.react('one',{deliver:false});await f.daemon.pollOnce();
 await f.clock.advance(REACTION_GRACE_MS-1000);await f.react('one',{removed:true,deliver:false});await f.clock.advance(1000);
 assert.equal((await f.saved()).status,'pending');assert.equal(f.entry.reactionDraft,null);
});

test('undelivered multi-select check removal before expiry cancels confirmation',async t=>{
 const f=await fixture(t,{multi:true});await f.react('one',{deliver:false});await f.react('white_check_mark',{deliver:false});await f.daemon.pollOnce();
 await f.clock.advance(REACTION_GRACE_MS-1000);await f.react('white_check_mark',{removed:true,deliver:false});await f.clock.advance(1000);
 assert.equal((await f.saved()).status,'pending');assert.equal(f.entry.reactionDraft,null);
});

test('undelivered removal with a remaining multi-selection restarts grace before confirming the remaining number',async t=>{
 const f=await fixture(t,{multi:true});for(const name of ['one','two','white_check_mark'])await f.react(name,{deliver:false});await f.daemon.pollOnce();
 await f.clock.advance(REACTION_GRACE_MS-1000);await f.react('one',{removed:true,deliver:false});await f.clock.advance(1000);
 assert.equal((await f.saved()).status,'pending');await f.clock.advance(REACTION_GRACE_MS-1);assert.equal((await f.saved()).status,'pending');await f.clock.advance(1);
 assert.deepEqual(ids(await f.saved()),['reject']);
});

for(const method of ['conversations.history','conversations.replies'])test(`expiry fails closed when ${method} fails and resumes only after fresh history succeeds`,async t=>{
 const f=await fixture(t);await f.react('one',{deliver:false});await f.daemon.pollOnce();f.api.failMethods.add(method);
 await f.clock.advance(REACTION_GRACE_MS);assert.equal((await f.saved()).status,'pending');assert.ok(f.entry.reactionDraft);assert.equal(f.entry.answerPending,false);
 await f.daemon.pollOnce();assert.equal((await f.saved()).status,'pending');f.api.failMethods.delete(method);await f.daemon.pollOnce();assert.deepEqual(ids(await f.saved()),['approve']);
});


test('a number first discovered at expiry gets a new full grace period and includes its pending comment',async t=>{
 const f=await fixture(t);await f.react('one',{deliver:false});await f.daemon.pollOnce();await f.clock.advance(REACTION_GRACE_MS-1000);
 await f.react('two',{deliver:false});await f.reply('追加条件',{deliver:false});await f.clock.advance(1000);
 assert.equal((await f.saved()).status,'pending');assert.equal(f.entry.reactionDraft.text,'追加条件');assert.deepEqual(f.entry.reactionDraft.optionIds,[]);
 await f.clock.advance(REACTION_GRACE_MS);assert.equal((await f.saved()).answer.text,'追加条件');assert.deepEqual(ids(await f.saved()),[]);
});
