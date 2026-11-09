import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec = promisify(execFile);

// Compile the actual AppKit host and exercise three foreground transitions.
test('native activation, reopen and selection preserve window behavior', async t => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'dialog-activation-'));
  t.after(() => fs.rm(root, {recursive:true, force:true}));
  const dir = path.resolve('apps/mac-dialog-decisions'), helper = path.join(root, 'check');
  const spec = name => JSON.stringify({window_title:name,title:name,options:[],multi_select:false,allow_text:false});
  const first = path.join(root,'first.json'), second = path.join(root,'second.json');
  await fs.writeFile(first,spec('first')); await fs.writeFile(second,spec('second'));
  await exec('swiftc', [...['dialog.swift','host.swift','preview.swift','unicode-width.swift','test/activation/main.swift'].map(f=>path.join(dir,f)), '-o',helper], {timeout:60000});
  for (const scenario of ['activate','reopen','selection']) {
    const result = await exec(helper,[first,scenario,second],{timeout:10000});
    assert.match(result.stdout, new RegExp(`${scenario} .*pass=true`));
  }
  const reopen = path.join(root, 'reopen-focus');
  await exec('swiftc', [...['dialog.swift','host.swift','preview.swift','unicode-width.swift','test/reopen-focus/main.swift'].map(f=>path.join(dir,f)), '-o',reopen], {timeout:60000});
  const result = await exec(reopen,[root],{timeout:30000});
  for (const scenario of ['external','selected']) {
    assert.match(result.stdout, new RegExp(`REOPEN_FOCUS scenario=${scenario} .*key=true`));
    assert.match(result.stdout, new RegExp(`REOPEN_CLICK_ORDER_PASS scenario=${scenario}`));
  }
  assert.match(result.stdout, /REOPEN_FOCUS_PASS/);
  console.log(result.stdout.split('\n').filter(line => /REOPEN_/.test(line)).join('\n'));
});
