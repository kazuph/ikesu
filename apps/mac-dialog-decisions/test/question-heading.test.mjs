import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import {spawnSync} from 'node:child_process';
import {buildDialogSpec} from '../spec.mjs';
import {headingCases} from './question-heading/cases.mjs';

test('agent questions keep a distinct heading and exactly one unabridged question body',()=>{
 for(const row of headingCases()){
  const spec=buildDialogSpec(row.decision,{id:'local'});
  assert.equal(spec.title,row.heading,row.name);assert.equal(spec.body,row.body,row.name);
  if(row.decision.kind==='agent_prompt')assert.equal(spec.question_label,'',row.name);
 }
});

// Compiles and renders the actual AppKit controller. Compiler startup exceeds five seconds.
test('actual panel renders question text once, with conditional bold heading in both appearances',()=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-question-heading-'));
 try{
  const rows=headingCases();
  if(process.env.IKESU_HEADING_DECISION_FIXTURE){const decision=JSON.parse(fs.readFileSync(process.env.IKESU_HEADING_DECISION_FIXTURE));rows.push({name:'live-p381',decision,heading:'',body:decision.body});}
  for(const row of rows)row.spec=buildDialogSpec(row.decision,{id:'local'});
  fs.writeFileSync(path.join(root,'cases.json'),JSON.stringify(rows));
  const dir=path.resolve('apps/mac-dialog-decisions');
  const result=spawnSync('swiftc',[...['dialog.swift','host.swift','unicode-width.swift','preview.swift','test/question-heading/main.swift'].map(p=>path.join(dir,p)),'-o',path.join(root,'check')],{encoding:'utf8',timeout:60000});
  assert.equal(result.status,0,result.stderr);
  const run=spawnSync(path.join(root,'check'),[path.join(root,'cases.json'),root],{encoding:'utf8',timeout:60000});
  const out=process.env.IKESU_HEADING_EVIDENCE;
  if(out){fs.mkdirSync(out,{recursive:true});for(const p of fs.readdirSync(root))if(/\.(png|json)$/.test(p))fs.copyFileSync(path.join(root,p),path.join(out,p));fs.writeFileSync(path.join(out,'native.log'),run.stdout+run.stderr);}
  assert.equal(run.status,0,run.stdout+run.stderr);console.log(run.stdout.trim());
 }finally{fs.rmSync(root,{recursive:true,force:true});}
});
