import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {AttachmentStore} from '../attachments.mjs';
for(const status of ['answered','cancelled','expired']) test(`unconfirmed ${status} cleanup survives restart and retains the first resolution time`,async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-resolution-'));
 try {
  const store=new AttachmentStore(root);
  await store.queueResolution('mini','decision',status,1000);
  await store.queueResolution('mini','decision',status,2000);
  const restarted=new AttachmentStore(root);
  assert.deepEqual(await restarted.pendingResolutions('mini'),[{machine:'mini',id:'decision',status,now:1000}]);
  assert.equal((await fs.stat(path.join(root,'resolutions/mini/decision/resolution.json'))).mode&0o777,0o600);
  await restarted.finishResolution('mini','decision');
  assert.deepEqual(await restarted.pendingResolutions(),[]);
 } finally {await fs.rm(root,{recursive:true,force:true});}
});

test('incomplete resolution files are retained and reported without hiding a healthy record',async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-incomplete-')),issues=[];
 try {
  for(const [id,data] of [['directory',null],['empty',''],['partial','{"status":']]) {
   const dir=path.join(root,'resolutions/local',id);await fs.mkdir(dir,{recursive:true});if(data!==null)await fs.writeFile(path.join(dir,'resolution.json'),data);
  }
  const store=new AttachmentStore(root,{log:message=>issues.push(message)});await store.queueResolution('local','healthy','cancelled',1000);
  assert.deepEqual(await store.pendingResolutions(),[{machine:'local',id:'healthy',status:'cancelled',now:1000}]);
  assert.equal(issues.length,3);for(const id of ['directory','empty','partial'])assert.ok(issues.some(message=>message.includes(id)),id);
  assert.equal(await fs.readFile(path.join(root,'resolutions/local/empty/resolution.json'),'utf8'),'');
  assert.equal(await fs.readFile(path.join(root,'resolutions/local/partial/resolution.json'),'utf8'),'{"status":');
  assert.equal((await fs.stat(path.join(root,'resolutions/local/directory'))).isDirectory(),true);
 }finally{await fs.rm(root,{recursive:true,force:true});}
});

test('actual interrupted journal write never publishes an incomplete final record',async()=>{
 const {execFile}=await import('node:child_process'),{promisify}=await import('node:util');const exec=promisify(execFile);
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-write-failure-'));
 try {
  const script=path.join(root,'write.mjs');await fs.writeFile(script,`import {AttachmentStore} from ${JSON.stringify(new URL('../attachments.mjs',import.meta.url).href)};await new AttachmentStore(process.argv[2]).queueResolution('local','interrupted','cancelled',1000);`);
  await assert.rejects(exec('/bin/sh',['-c','ulimit -f 0; exec "$@"','journal-write',process.execPath,script,root]));
  const dir=path.join(root,'resolutions/local/interrupted');
  await assert.rejects(fs.stat(path.join(dir,'resolution.json')),{code:'ENOENT'});
  assert.ok((await fs.readdir(dir)).some(name=>name.endsWith('.writing')),'interrupted bytes must be retained separately');
  const issues=[],restarted=new AttachmentStore(root,{log:message=>issues.push(message)});
  assert.deepEqual(await restarted.pendingResolutions(),[]);assert.equal(issues.length,1);
  await restarted.queueResolution('local','interrupted','cancelled',1000);
  assert.equal((await restarted.pendingResolutions())[0].now,1000);
 }finally{await fs.rm(root,{recursive:true,force:true});}
});
