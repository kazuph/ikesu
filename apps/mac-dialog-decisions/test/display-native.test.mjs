import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile);

// Compiles the actual AppKit controller and starts both media engines. The
// compiler/codec startup makes this integration check longer than five seconds.
test('actual native panel: layout, fixed choices, comment transitions, four video formats, and window ordering',async t=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-display-native-'));
 t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const options={timeout:60000,maxBuffer:8*1024*1024};
 await exec('taskpolicy',['-b','ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i','color=c=blue:s=160x90:r=10','-t','1','-c:v','libx264','-pix_fmt','yuv420p','-y',path.join(root,'sample.mp4')],options);
 for(const format of ['mov','m4v']) await exec('taskpolicy',['-b','ffmpeg','-hide_banner','-loglevel','error','-i',path.join(root,'sample.mp4'),'-c','copy','-y',path.join(root,'sample.'+format)],options);
 await exec('taskpolicy',['-b','ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i','color=c=blue:s=160x90:r=10','-t','1','-c:v','libvpx-vp9','-y',path.join(root,'sample.webm')],options);
 const dir=path.resolve('apps/mac-dialog-decisions');
 const helper=path.join(root,'display-native');
 await exec('swiftc',[...['dialog.swift','host.swift','preview.swift','unicode-width.swift','test/display-native/main.swift'].map(name=>path.join(dir,name)),'-o',helper],options);
 const result=await exec(helper,[root],options);
 assert.match(result.stdout,/DISPLAY_NATIVE_PASS/);
 for(const format of ['mp4','mov','m4v','webm']) assert.match(result.stdout,new RegExp(`VIDEO_PLAY_PAUSE_PASS format=${format}`));
 const rows=JSON.parse(await fs.readFile(path.join(root,'native-metrics.json'),'utf8'));
 assert.equal(rows.length,4);assert.ok(rows.every(row=>row.size[0]>row.size[1]&&row.choices===3));
 const evidence=process.env.IKESU_DISPLAY_EVIDENCE;
 if(evidence){await fs.mkdir(evidence,{recursive:true});for(const file of await fs.readdir(root))if(/\.(png|json)$/.test(file))await fs.copyFile(path.join(root,file),path.join(evidence,file));await fs.writeFile(path.join(evidence,'display-native.log'),result.stdout+result.stderr);}
 console.log(result.stdout.split('\n').filter(line=>/PASS|ORDER/.test(line)).join('\n'));
});
