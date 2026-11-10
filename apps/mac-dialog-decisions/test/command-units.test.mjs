import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {buildDialogSpec} from '../spec.mjs';

const units=[
  {op:'',text:'cd /x/metan',label:'移動',verdict:'allow'},
  {op:'&&',text:'bash render_mini.sh metan',label:'実行',verdict:'ask',rule:'rm-paths',reason:'作業フォルダの外を消す',target:'rm -rf out/'},
  {op:'|',text:'tail -5',label:'読む',verdict:'allow'},
];
const command='cd /x/metan && bash render_mini.sh metan | tail -5';
const make=(extra={})=>({decision_id:'d1',kind:'command_guard',title:'確認',body:'申請文です',
  options:[{id:'approve',label:'承認'},{id:'deny',label:'却下'}],
  origin:{command,target_command:'bash render_mini.sh metan',guard_rule:'rm-paths',guard_reason:'外を消す',command_units:units,...extra}});

test('units become rows and replace the whole-command view', () => {
  const spec=buildDialogSpec(make(),{id:'local'},0);
  assert.deepEqual(spec.approval_units.map(u=>u.verdict),['allow','ask','allow']);
  assert.equal(spec.approval_units_heading,'3行のうち 2行目が確認対象');
  assert.equal(spec.approval_command,command);
  assert.equal(spec.body,'申請文です');
});

test('two ask lines list both numbers', () => {
  const two=units.map((u,i)=>i===2?{...u,verdict:'block',reason:'x'}:u);
  const spec=buildDialogSpec(make({command_units:two}),{id:'local'},0);
  assert.equal(spec.approval_units_heading,'3行のうち 2・3行目が確認対象');
});

test('no ask line says the line cannot be identified with rule and reason', () => {
  const none=units.map(u=>({...u,verdict:'allow'}));
  const spec=buildDialogSpec(make({command_units:none}),{id:'local'},0);
  assert.equal(spec.approval_units_heading,'行を特定できない（規則 rm-paths）\n外を消す');
});

test('malformed or unknown units fall back to the existing view', () => {
  for (const bad of [[{...units[0],verdict:'maybe'}],[{...units[0],op:'&'}],[{op:'',text:1,label:'x',verdict:'allow'}],[null],[],'x']) {
    const spec=buildDialogSpec(make({command_units:bad}),{id:'local'},0);
    assert.equal(spec.approval_units,null);
    assert.equal(spec.approval_units_heading,'');
    assert.equal(spec.approval_command,command);
  }
  const absent=make(); delete absent.origin.command_units;
  assert.equal(buildDialogSpec(absent,{id:'local'},0).approval_command,command);
});

test('native panel draws rows (compiles dialog.swift)', () => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'ikesu-command-units-'));
  try {
    const dir=path.resolve('apps/mac-dialog-decisions');
    for (const [name,spec] of [['basic',buildDialogSpec(make(),{id:'local'},0)],
      ['none',buildDialogSpec(make({command_units:units.map(u=>({...u,verdict:'allow'}))}),{id:'local'},0)]]) {
      fs.writeFileSync(path.join(root,name+'.json'),JSON.stringify(spec));
    }
    fs.copyFileSync(path.join(dir,'test/command-units/main.swift'),path.join(root,'main.swift'));
    const compile=spawnSync('swiftc',[path.join(dir,'dialog.swift'),path.join(dir,'unicode-width.swift'),path.join(dir,'preview.swift'),path.join(root,'main.swift'),'-o',path.join(root,'check')],{encoding:'utf8',timeout:120000});
    assert.equal(compile.status,0,compile.stderr);
    for (const name of ['basic','none']) {
      const out=path.join(root,name); fs.mkdirSync(out);
      const run=spawnSync(path.join(root,'check'),[path.join(root,name+'.json'),out],{encoding:'utf8',timeout:60000});
      assert.equal(run.status,0,run.stdout+run.stderr);
    }
    const evidence=process.env.IKESU_UNITS_EVIDENCE;
    if (evidence) { fs.mkdirSync(evidence,{recursive:true}); fs.copyFileSync(path.join(root,'basic/units-NSAppearanceNameAqua.png'),path.join(evidence,'units-mac.png')); fs.copyFileSync(path.join(root,'basic/units-NSAppearanceNameDarkAqua.png'),path.join(evidence,'units-mac-dark.png')); }
  } finally {fs.rmSync(root,{recursive:true,force:true});}
});
