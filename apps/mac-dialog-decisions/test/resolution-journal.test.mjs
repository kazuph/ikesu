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
