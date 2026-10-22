import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

for(const layout of ['missing-app','unsigned-app']) {
 test(`配布版の${layout}を実codesignで拒否しSwiftへ戻らない`,async t=>{
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'panel-release-'));
  t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
  const source=new URL('../',import.meta.url);
  for(const name of fs.readdirSync(source).filter(name=>name.endsWith('.mjs')))fs.copyFileSync(new URL(name,source),path.join(root,name));
  fs.writeFileSync(path.join(root,'CHANNEL'),'release\n');
  if(layout==='unsigned-app'){
   const executable=path.join(root,'IkesuDecisionDialog.app','Contents','MacOS','ikesu-decision-dialog');
   fs.mkdirSync(path.dirname(executable),{recursive:true});
   assert.ok(process.env.MAC_DIALOG_TEST_NATIVE_BINARY,'actual compiled AppKit binary is required');
   fs.copyFileSync(process.env.MAC_DIALOG_TEST_NATIVE_BINARY,executable);fs.chmodSync(executable,0o700);
  }
  const {ensureNativeDialog}=await import(pathToFileURL(path.join(root,'mac-dialog-decisions.mjs')));
  const messages=[];
  assert.equal(ensureNativeDialog({stateDir:path.join(root,'state')},message=>messages.push(message)),null);
  assert.ok(messages.some(message=>message.includes('検証に失敗')));
  assert.equal(fs.existsSync(path.join(root,'state','bin')),false);
 });
}
