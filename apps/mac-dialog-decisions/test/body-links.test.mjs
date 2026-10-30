import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile);

// Compiling the real AppKit controller makes this integration test exceed five seconds.
test('inactive panel opens URL, image and video from actual body mouse clicks',async t=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'ikesu-body-links-'));
 t.after(()=>fs.rm(root,{recursive:true,force:true}));
 await fs.writeFile(path.join(root,'image.png'),Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6AAAAAElFTkSuQmCC','base64'));
 await exec('taskpolicy',['-b','ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i','color=c=blue:s=160x90:r=10','-t','1','-c:v','libx264','-pix_fmt','yuv420p','-y',path.join(root,'video.mp4')],{timeout:60000});
 const dir=path.resolve('apps/mac-dialog-decisions'), bin=path.join(root,'body-links');
 await exec('swiftc',['dialog.swift','host.swift','preview.swift','unicode-width.swift','test/body-links/main.swift'].map(f=>path.join(dir,f)).concat(['-o',bin]),{timeout:60000});
 const result=await exec(bin,[root],{timeout:15000});
 assert.equal((result.stdout.match(/BODY_LINK_CLICK_PASS/g)||[]).length,4);
 t.diagnostic(result.stdout);
});
