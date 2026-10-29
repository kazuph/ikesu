import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {buildDialogSpec} from '../spec.mjs';

// Generates actual guard decisions, compiles AppKit, and renders 16 themed cases.
// Compiler startup makes this integration test longer than five seconds.
test('actual guard Decision flows through buildDialogSpec into exact native red ranges', () => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-target-command-'));
  try {
    const generate=spawnSync('go',['test','-count=1','-timeout','30s','./internal/guard','-run','^TestTargetCommandDecisionFixtures$'],{
      encoding:'utf8',env:{...process.env,IKESU_PANEL_DECISION_FIXTURE_DIR:root},timeout:60000,
    });
    assert.equal(generate.status,0,generate.stdout+generate.stderr);
    const decisions=JSON.parse(fs.readFileSync(path.join(root,'decisions.json'),'utf8'));
    assert.equal(Object.keys(decisions).length,8);
    const rows=Object.entries(decisions).map(([name,decision])=>{
      const spec=buildDialogSpec(decision,{id:'local'},0);
      assert.equal(spec.body,decision.body);
      assert.equal(spec.title,decision.title);
      const target=spec.details.filter(detail=>detail.label==='対象コマンド');
      assert.equal(target.length,name==='same'?0:1);
      if (target.length) assert.equal(target[0].value,decision.origin.target_command);
      return {name,decision,spec};
    });
    fs.writeFileSync(path.join(root,'cases.json'),JSON.stringify(rows));
    const dir=path.resolve('apps/mac-dialog-decisions');
    fs.copyFileSync(path.join(dir,'test/target-command/main.swift'),path.join(root,'main.swift'));
    const compile=spawnSync('swiftc',[path.join(dir,'dialog.swift'),path.join(dir,'unicode-width.swift'),path.join(dir,'preview.swift'),path.join(root,'main.swift'),'-o',path.join(root,'check')],{encoding:'utf8',timeout:60000});
    assert.equal(compile.status,0,compile.stderr);
    const run=spawnSync(path.join(root,'check'),[path.join(root,'cases.json'),root],{encoding:'utf8',env:process.env,timeout:60000});
    assert.equal(run.status,0,run.stdout+run.stderr);
    console.log(run.stdout.trim());
    const evidence=process.env.IKESU_TARGET_COLOR_EVIDENCE;
    if(evidence){
      fs.mkdirSync(evidence,{recursive:true});
      for(const file of fs.readdirSync(root).filter(file=>file.endsWith('.png')||file.endsWith('.json'))) fs.copyFileSync(path.join(root,file),path.join(evidence,file));
    }
  } finally {fs.rmSync(root,{recursive:true,force:true});}
});
