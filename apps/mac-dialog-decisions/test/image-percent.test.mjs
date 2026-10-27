import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {discoverRichContent} from '../rich.mjs';
import {RichService} from '../rich-service.mjs';
const exec=promisify(execFile);

test('literal percent filenames stay literal while only Markdown destinations decode URL escapes',async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-percent-'));
 try {
  const swift='import AppKit;import Foundation;let root=CommandLine.arguments[1];for (name,symbol) in [("result%20final.png","checkmark"),("result final.png","xmark")]{let image=NSImage(systemSymbolName:symbol,accessibilityDescription:nil)!;let bitmap=NSBitmapImageRep(data:image.tiffRepresentation!)!;try bitmap.representation(using:.png,properties:[:])!.write(to:URL(fileURLWithPath:root).appendingPathComponent(name))}';
  await exec('/usr/bin/swift',['-e',swift,root]);
  const percent=path.join(root,'result%20final.png'),space=path.join(root,'result final.png'),single=path.join(root,'100%.png');
  const percentBytes=await fs.readFile(percent),spaceBytes=await fs.readFile(space);assert.notDeepEqual(percentBytes,spaceBytes);await fs.copyFile(percent,single);
  const service=new RichService({stateDir:path.join(root,'state')});
  for(const body of [percent,'画像: '+percent,'`'+percent+'`','"'+percent+'"','結果は '+percent+' です',single]) {
   const wanted=body===single?single:percent;const found=discoverRichContent({body});assert.deepEqual(found.images,[wanted]);
   assert.deepEqual(await service.readImage('local',found.images[0]),percentBytes);
  }
  for(const body of ['[画像]('+percent+')','![画像](<'+percent+'>)']) {
   const found=discoverRichContent({body});assert.deepEqual(found.images,[space]);assert.deepEqual(await service.readImage('local',found.images[0]),spaceBytes);
  }
  assert.deepEqual(discoverRichContent({body:'[画像]('+single.replace('%','%25')+')'}).images,[single]);
 }finally{await fs.rm(root,{recursive:true,force:true});}
});
