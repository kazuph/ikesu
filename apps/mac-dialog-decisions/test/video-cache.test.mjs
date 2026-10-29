import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {RichService} from '../rich-service.mjs';
import {RICH_LIMITS} from '../rich.mjs';
const exec=promisify(execFile);

test('video cache streams regular files without inheriting the image attachment size limit, rejects FIFO/symlinks, and cleans cancelled cache',async t=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-video-'));t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const service=new RichService({stateDir:root});
 const source=path.join(root,'日本語 動画.mp4');
 const file=await fs.open(source,'w');await file.truncate(RICH_LIMITS.attachmentBytes+1);await file.close();
 const cached=await service.cacheVideo('local',source,new AbortController().signal);
 assert.equal((await fs.stat(cached)).size,RICH_LIMITS.attachmentBytes+1);assert.equal((await fs.stat(cached)).mode&0o777,0o600);
 const fifo=path.join(root,'fifo.mp4');await exec('/usr/bin/mkfifo',[fifo]);
 await assert.rejects(service.cacheVideo('local',fifo,new AbortController().signal),/通常ファイル/);
 const link=path.join(root,'link.mp4');await fs.symlink(source,link);await assert.rejects(service.cacheVideo('local',link,new AbortController().signal));
 const abort=new AbortController();abort.abort();await assert.rejects(service.cacheVideo('local',source,abort.signal));
 const published=[],lifetime=new AbortController();
 await service.load({body:'動画: '+source},'local',lifetime.signal,message=>published.push(message));
 const video=published.find(message=>message.status==='rich_video');assert.ok(video);assert.equal(video.source,source);assert.equal((await fs.stat(video.path)).size,RICH_LIMITS.attachmentBytes+1);
 lifetime.abort();
 const deadline=Date.now()+2000;while(await fs.stat(video.path).then(()=>true,()=>false)){assert.ok(Date.now()<deadline);await new Promise(resolve=>setTimeout(resolve,5));}
 assert.deepEqual((await fs.readdir(path.join(root,'rich-cache'))),[path.basename(cached)]);
});
