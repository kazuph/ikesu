import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile);

// Compiling and opening the actual AppKit panel takes longer than five seconds.
test('click then command shortcuts select and send exactly once', async t => {
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'ikesu-shortcut-'));
 t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const dir=path.resolve('apps/mac-dialog-decisions');
 const main=path.join(root,'main.swift');
 await fs.copyFile(path.join(dir,'test/shortcut/main.swift'),main);
 const binary=path.join(root,'shortcut');
 await exec('swiftc',[...['dialog.swift','unicode-width.swift','preview.swift'].map(name=>path.join(dir,name)),main,'-o',binary],{timeout:60000});
 const result=await exec(binary,[root],{timeout:60000});
 assert.match(result.stdout,/SHORTCUT_AFTER_CMD1 option=approve sends=0/);
 assert.match(result.stdout,/SHORTCUT_AFTER_CMD_RETURN option=approve sends=1/);
 console.log(result.stdout);
});
