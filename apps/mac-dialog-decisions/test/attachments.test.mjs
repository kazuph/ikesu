import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { AttachmentStore } from '../attachments.mjs';

test('original attachments are private, machine-scoped, retained and expired by status', async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-rich-'));
 try {
  const store=new AttachmentStore(root); const draft=await store.draftDirectory('local','d1');
  const source=path.join(draft,'日本語 画像.png'); const bytes=Buffer.from('original-format'); await fs.writeFile(source,bytes);
  const files=await store.save('local','d1',[{path:source,name:'日本語 画像.png'}]);
  assert.equal(files.length,1); assert.deepEqual(await fs.readFile(files[0]),bytes);
  assert.equal((await fs.stat(files[0])).mode&0o777,0o600); assert.equal((await fs.stat(path.dirname(files[0]))).mode&0o777,0o700);
  await store.resolve('local','d1','answered',1000); await store.cleanup(1000+7*86400000-1); assert.equal((await fs.stat(files[0])).isFile(),true);
  await store.cleanup(1000+7*86400000); await assert.rejects(fs.stat(files[0]),{code:'ENOENT'});
  await store.draftDirectory('mini','d2'); await store.resolve('mini','d2','cancelled'); await assert.rejects(fs.stat(path.join(root,'drafts','mini','d2')),{code:'ENOENT'});
 } finally {await fs.rm(root,{recursive:true,force:true});}
});

test('unsafe IDs, source escape, symlinks, excess count and bytes never save',async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-rich-'));
 try {
 const store=new AttachmentStore(root); await assert.rejects(store.draftDirectory('../other','d1'));
 const draft=await store.draftDirectory('local','d1'); const outside=path.join(root,'outside.png'); await fs.writeFile(outside,'secret'); await fs.symlink(outside,path.join(draft,'link.png'));
 for(const p of [outside,path.join(draft,'link.png')]) await assert.rejects(store.save('local','d1',[{path:p,name:'1.png'}]));
 await assert.rejects(store.save('local','d1',Array(5).fill({path:outside,name:'1.png'})));
 const big=path.join(draft,'big.png'); const handle=await fs.open(big,'w'); await handle.truncate(20*1024*1024+1); await handle.close();
 await assert.rejects(store.save('local','d1',[{path:big,name:'big.png'}]));
 await fs.mkdir(path.join(root,'attachments'),{recursive:true}); await fs.symlink(root,path.join(root,'attachments','evil'));
 await assert.rejects(store.save('evil','d1',[]));
 } finally {await fs.rm(root,{recursive:true,force:true});}
});
