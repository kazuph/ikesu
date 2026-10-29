import test from 'node:test';import assert from 'node:assert/strict';
import http from 'node:http';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {SlackClient,SlackDecisionsDaemon} from '../slack-decisions.mjs';
import {headingCases} from '../../mac-dialog-decisions/test/question-heading/cases.mjs';

test('real Slack daemon posts each question once to a loopback API, preserving distinct headings',async()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-heading-slack-'));const requests=[];
 const server=http.createServer(async(req,res)=>{const chunks=[];for await(const c of req)chunks.push(c);const payload=JSON.parse(Buffer.concat(chunks).toString());requests.push({method:req.url,payload});res.setHeader('content-type','application/json');res.end(JSON.stringify({ok:true,ts:String(requests.length)}));});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const rows=headingCases();if(process.env.IKESU_HEADING_DECISION_FIXTURE){const decision=JSON.parse(fs.readFileSync(process.env.IKESU_HEADING_DECISION_FIXTURE));rows.push({name:'live-p381',decision,heading:'',body:decision.body});}
 const slack=new SlackClient({token:'local-test-only',channel:'ISOLATED',apiBase:`http://127.0.0.1:${server.address().port}`});
 const daemon=new SlackDecisionsDaemon({config:{},slack,postsFilePath:path.join(root,'posts.json'),log:()=>{}});const metrics=[];
 try{
  for(const row of rows){
   const decision={...row.decision,decision_id:'isolated-'+row.name};const entry={decision,machine:{id:'local'},machineId:'local',key:'local\0'+decision.decision_id};
   await daemon.postDecision(entry);
   assert.ok(entry.post,'actual POST completed');
   const post=requests.filter(r=>r.method==='/chat.postMessage').at(-1).payload;
   const sections=post.blocks.filter(b=>b.type==='section').map(b=>b.text.text);
   const bodyCopies=sections.reduce((n,s)=>n+s.split(row.body).length-1,0);
   const bold=sections.filter(s=>s==='*'+row.body+'*').length;
   metrics.push({case:row.name,bodyCopies,boldQuestionCopies:bold,heading:row.heading,post});
  }
  const out=process.env.IKESU_HEADING_SLACK_EVIDENCE;
  if(out){fs.mkdirSync(out,{recursive:true});fs.writeFileSync(path.join(out,'requests.json'),JSON.stringify(requests,null,2));fs.writeFileSync(path.join(out,'metrics.json'),JSON.stringify(metrics,null,2));fs.copyFileSync(path.join(root,'posts.json'),path.join(out,'posts.json'));}
  for(const [i,row] of rows.entries()){
   const m=metrics[i];assert.equal(m.bodyCopies,1,row.name+' duplicate body');assert.equal(m.boldQuestionCopies,0,row.name+' question as heading');
   const sections=m.post.blocks.filter(b=>b.type==='section').map(b=>b.text.text);
   if(row.heading)assert.ok(sections.includes('*'+row.heading+'*'),row.name);
   assert.equal(m.post.text.split(row.body).length-1,1,row.name+' fallback duplicates');
   if(row.name==='guard')assert.ok(sections.includes('*実行するコマンド*\n```\nrm /tmp/isolated-example\n```'));
  }
 }finally{await new Promise(resolve=>server.close(resolve));fs.rmSync(root,{recursive:true,force:true});}
});
