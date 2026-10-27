import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile);
for(const operation of ['preview','attachment']) test(`actual unwritten FIFO is rejected without blocking ${operation}`,async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-fifo-'));
 try {
  const draft=path.join(root,'drafts/local/fifo');await fs.mkdir(draft,{recursive:true});const fifo=path.join(draft,'画像.png');await exec('/usr/bin/mkfifo',[fifo]);
  const service=new URL('../rich-service.mjs',import.meta.url).href;
  const script=`import {RichService} from ${JSON.stringify(service)}; const s=new RichService({stateDir:${JSON.stringify(root)}});try {${operation==='preview'?`await s.readImage('local',${JSON.stringify(fifo)},new AbortController().signal)`:`await s.store.save('local','fifo',[{path:${JSON.stringify(fifo)}}])`};process.exitCode=1;}catch(error){if(!/通常|regular/.test(error.message))throw error;console.log('FIFO_REJECTED');}`;
  const result=await exec(process.execPath,['--input-type=module','-e',script],{timeout:2000});assert.match(result.stdout,/FIFO_REJECTED/);
 } finally {await fs.rm(root,{recursive:true,force:true});}
});
