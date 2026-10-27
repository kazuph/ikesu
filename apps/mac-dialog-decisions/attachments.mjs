import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { RICH_LIMITS, imagePath } from './rich.mjs';

export function safeID(value) { if(typeof value!=='string'||!value||!/^[-_a-zA-Z0-9.]+$/.test(value)||value==='.'||value==='..') throw Error('保存先の ID が不正です'); return value; }
export async function readBounded(file,limit) {
 const buffer=Buffer.alloc(limit+1);let used=0;
 while(used<buffer.length) {const result=await file.read(buffer,used,buffer.length-used,null);if(!result.bytesRead)break;used+=result.bytesRead;}
 if(used>limit) throw Error("ファイルの容量上限を超えています");
 return buffer.subarray(0,used);
}
export class AttachmentStore {
 constructor(stateDir) {this.root=path.resolve(stateDir);}
 async readResolution(file) {
  const handle=await fs.open(file,constants.O_RDONLY|constants.O_NOFOLLOW|constants.O_NONBLOCK);
  try {
   if(!(await handle.stat()).isFile()) throw Error('整理待ちの記録が通常ファイルではありません');
   const record=JSON.parse(String(await readBounded(handle,RICH_LIMITS.pageBytes)));
   safeID(record.machine);safeID(record.id);
   if(!['answered','cancelled','expired'].includes(record.status)||!Number.isFinite(record.now)) throw Error('整理待ちの記録が不正です');
   return record;
  } finally {await handle.close();}
 }
 async queueResolution(machine,id,status,now) {
  safeID(machine);safeID(id);
  if(!['answered','cancelled','expired'].includes(status)||!Number.isFinite(now)) throw Error('解決状態が不正です');
  const dir=await this.directory('resolutions',machine,id);const file=path.join(dir,'resolution.json');
  let handle;
  try {handle=await fs.open(file,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600);}
  catch(error) {if(error.code!=='EEXIST')throw error;return this.readResolution(file);}
  const record={machine,id,status,now};
  try {await handle.writeFile(JSON.stringify(record));await handle.sync();} finally {await handle.close();}
  return record;
 }
 async pendingResolutions(machine) {
  if(machine!==undefined)safeID(machine);
  const base=path.join(this.root,'resolutions');let machines;
  try {machines=await fs.readdir(base,{withFileTypes:true});}catch(error){if(error.code==='ENOENT')return [];throw error;}
  const records=[];
  for(const m of machines.filter(m=>m.isDirectory()&&!m.isSymbolicLink()&&(machine===undefined||m.name===machine))) {
   const dir=path.join(base,m.name);
   for(const d of await fs.readdir(dir,{withFileTypes:true})) {
    if(!d.isDirectory()||d.isSymbolicLink())continue;
    const record=await this.readResolution(path.join(dir,d.name,'resolution.json'));
    if(record.machine!==m.name||record.id!==d.name)throw Error('整理待ちの保存先が一致しません');
    records.push(record);
   }
  }
  return records;
 }
 async finishResolution(machine,id) {
  safeID(machine);safeID(id);
  await fs.rm(path.join(this.root,'resolutions',machine,id),{recursive:true,force:true});
 }
 async directory(kind,machine,id) {
  safeID(machine); safeID(id);
  await fs.mkdir(this.root,{recursive:true,mode:0o700});
  if((await fs.lstat(this.root)).isSymbolicLink()) throw Error("状態フォルダにシンボリックリンクは使えません");
  let dir=this.root;
  for(const part of [kind,machine,id]) {
   dir=path.join(dir,part);
   try {await fs.mkdir(dir,{mode:0o700});} catch(e) {if(e.code!=='EEXIST') throw e;}
   const stat=await fs.lstat(dir); if(!stat.isDirectory()||stat.isSymbolicLink()) throw Error('保存先にシンボリックリンクは使えません');
   await fs.chmod(dir,0o700);
  }
  return dir;
 }
 draftDirectory(machine,id) {return this.directory('drafts',machine,id);}
 async save(machine,id,attachments) {
  if(!Array.isArray(attachments)||attachments.length>RICH_LIMITS.images) throw Error('添付は4枚までです');
  const dir=await this.directory('attachments',machine,id);
  const draft=await this.draftDirectory(machine,id);
  const saved=[];
  for(let i=0;i<attachments.length;i++) {
   const item=attachments[i]; const source=path.resolve(item.path||'');
   if(path.dirname(source)!==draft||!imagePath(source)) throw Error('添付の下書きパスが不正です');
   const input=await fs.open(source,constants.O_RDONLY|constants.O_NOFOLLOW|constants.O_NONBLOCK);
   try {
    const stat=await input.stat(); if(!stat.isFile()) throw Error('通常ファイルだけを添付できます');
    if(stat.size>RICH_LIMITS.attachmentBytes) throw Error('添付は1枚20MBまでです');
    const data=await readBounded(input,RICH_LIMITS.attachmentBytes); if(data.length>RICH_LIMITS.attachmentBytes) throw Error('添付は1枚20MBまでです');
    const filename=`${i+1}${path.extname(source).toLowerCase()}`;
    const destination=path.join(dir,filename);
    const output=await fs.open(destination,constants.O_WRONLY|constants.O_CREAT|constants.O_TRUNC|constants.O_NOFOLLOW,0o600);
    try {await output.chmod(0o600);await output.writeFile(data);} finally {await output.close();}
    saved.push(destination);
   } finally {await input.close();}
  }
  return saved;
 }
 async resolve(machine,id,status,now=Date.now()) {
  safeID(machine);safeID(id);
  if(status==='answered') {
   const dir=await this.directory('attachments',machine,id);
   const file=await fs.open(path.join(dir,'answered.json'),constants.O_WRONLY|constants.O_CREAT|constants.O_TRUNC|constants.O_NOFOLLOW,0o600);
   try {await file.writeFile(JSON.stringify({answered:now}));} finally {await file.close();}
  } else if(['cancelled','expired'].includes(status)) await fs.rm(path.join(this.root,'attachments',machine,id),{recursive:true,force:true});
  if(['answered','cancelled','expired'].includes(status)) await fs.rm(path.join(this.root,'drafts',machine,id),{recursive:true,force:true});
 }
 async cleanup(now=Date.now()) {
  const base=path.join(this.root,'attachments');
  let next=null;
  let machines; try {machines=await fs.readdir(base,{withFileTypes:true});} catch(e) {if(e.code==='ENOENT') return null;throw e;}
  for(const machine of machines.filter(m=>m.isDirectory()&&!m.isSymbolicLink())) {
   const dir=path.join(base,machine.name);
   for(const decision of await fs.readdir(dir,{withFileTypes:true})) {
    if(!decision.isDirectory()||decision.isSymbolicLink()) continue;
    const folder=path.join(dir,decision.name);
    try {
     const file=await fs.open(path.join(folder,'answered.json'),constants.O_RDONLY|constants.O_NOFOLLOW);
     let record;try {record=JSON.parse(await file.readFile('utf8'));} finally {await file.close();}
     if(Number.isFinite(record.answered)) {
      const deadline=record.answered+RICH_LIMITS.retentionMs;
      if(now>=deadline) await fs.rm(folder,{recursive:true,force:true});
      else next=next===null?deadline:Math.min(next,deadline);
     }
    } catch(e) {if(e.code!=='ENOENT') throw e;}
   }
  }
  return next;
 }
}
