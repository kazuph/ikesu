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
    assert.equal(Object.keys(decisions).length,9);
    const rows=Object.entries(decisions).map(([name,decision])=>{
      // This test covers the whole-command view; per-line rows are in command-units.test.mjs.
      assert.ok(Array.isArray(decision.origin.command_units)||decision.origin.command_units===undefined);
      delete decision.origin.command_units;
      const spec=buildDialogSpec(decision,{id:'local'},0);
      assert.equal(spec.body,decision.body);
      assert.equal(spec.title,decision.title);
      assert.equal(spec.approval_command,decision.origin.command);
      if (name==='requested-example') {
        assert.deepEqual(spec.approval_target_range,{start:0,length:decision.origin.target_command.length});
        assert.deepEqual(spec.approval_fields.map(field=>field.label),['対象','理由','内容','確認済み','戻し方']);
      }
      assert.equal(spec.details.some(detail=>['コマンド','対象コマンド','理由'].includes(detail.label)),false);
      return {name,decision,spec};
    });
    const claude={kind:'tool_permission',title:'Bash command',body:'Bash command\n確認理由\necho safe\n警告文',origin:{agent:'claude',tool:'Bash command',explanation:'確認理由',command:'echo safe',warning:'警告文'},options:[{id:'1',label:'Yes'},{id:'4',label:'No'}]};
    rows.push({name:'claude-permission',decision:claude,spec:buildDialogSpec(claude,{id:'local'},0)});
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

// Native window is not involved: the real guard output goes through buildDialogSpec with command_units intact.
test('actual guard Decision keeps its command_units through buildDialogSpec', () => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-target-units-'));
  try {
    const generate=spawnSync('go',['test','-count=1','-timeout','30s','./internal/guard','-run','^TestTargetCommandDecisionFixtures$'],{
      encoding:'utf8',env:{...process.env,IKESU_PANEL_DECISION_FIXTURE_DIR:root},timeout:60000,
    });
    assert.equal(generate.status,0,generate.stdout+generate.stderr);
    const decisions=JSON.parse(fs.readFileSync(path.join(root,'decisions.json'),'utf8'));
    const unquote=text=>text.replace(/['"]/g,'');
    for (const [name,decision] of Object.entries(decisions)) {
      const spec=buildDialogSpec(structuredClone(decision),{id:'local'},0);
      assert.ok(Array.isArray(spec.approval_units)&&spec.approval_units.length>0,`${name}: approval_units`);
      const ask=spec.approval_units.filter(unit=>unit.verdict==='ask'||unit.verdict==='block');
      assert.ok(ask.length>=1,`${name}: at least one ask row`);
      const target=decision.origin.target_command;
      if (target) {
        assert.ok(ask.some(unit=>unquote(unit.text).includes(unquote(target))||unit.target===target),`${name}: an ask row shows target_command`);
      }
      assert.match(spec.approval_units_heading,/行目が確認対象$/,`${name}: heading`);
      // Whole-command verdict without any ask row: the heading says the line cannot be identified.
      const none=structuredClone(decision);
      none.origin.command_units=none.origin.command_units.map(unit=>({...unit,verdict:'allow'}));
      assert.match(buildDialogSpec(none,{id:'local'},0).approval_units_heading,/^行を特定できない/,`${name}: unidentified heading`);
    }
  } finally {fs.rmSync(root,{recursive:true,force:true});}
});
