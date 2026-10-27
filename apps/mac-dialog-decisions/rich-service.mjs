import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { AttachmentStore, safeID, readBounded } from './attachments.mjs';
import { RICH_LIMITS, discoverRichContent, fetchPublic, pageMetadata, imagePath } from './rich.mjs';

function execute(file,args,{input,signal,maxBuffer=RICH_LIMITS.imageBytes}={}) {
 return new Promise((resolve,reject)=>{
  const child=execFile(file,args,{encoding:null,maxBuffer,signal},(error,stdout,stderr)=>error?reject(Error(String(stderr||error.message))):resolve(stdout));
  child.stdin.on('error',()=>{}); child.stdin.end(input);
 });
}
const quote=value=>"'"+String(value).replaceAll("'","'\\''")+"'";
const remoteScript=String.raw`
import os,sys,json,stat,base64,time
r=json.load(sys.stdin)
def safe(v):
 if not v or any(c not in 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_. ' for c in v) or v in ['.','..']: raise ValueError('invalid ID')
def directory(parts):
 d=os.path.expanduser('~')
 for p in ['.local','state','ikesu','plugins','kazuph.mac-dialog-decisions']+parts:
  safe(p); d=os.path.join(d,p)
  try: os.mkdir(d,0o700)
  except FileExistsError: pass
  if not stat.S_ISDIR(os.lstat(d).st_mode): raise ValueError('symlink directory')
  os.chmod(d,0o700)
 return d
if r['action']=='read':
 if not os.path.isabs(r['path']): raise ValueError('absolute path required')
 fd=os.open(r['path'],os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 with os.fdopen(fd,'rb') as f:
  info=os.fstat(f.fileno())
  if not stat.S_ISREG(info.st_mode): raise ValueError('regular image file required')
  if info.st_size>r['limit']: raise ValueError('image exceeds 10MB')
  data=f.read(r['limit']+1)
  if len(data)>r['limit']: raise ValueError('image exceeds 10MB')
  sys.stdout.buffer.write(data)
elif r['action']=='save':
 d=directory(['attachments',r['machine'],r['id']]); result=[]
 for i,item in enumerate(r['files']):
  ext=item['extension']
  if ext not in ['.png','.jpg','.jpeg','.gif','.webp']: raise ValueError('format')
  data=base64.b64decode(item['data'],validate=True)
  if len(data)>r['limit']: raise ValueError('attachment exceeds 20MB')
  dest=os.path.join(d,str(i+1)+ext)
  fd=os.open(dest,os.O_WRONLY|os.O_CREAT|os.O_TRUNC|os.O_NOFOLLOW,0o600)
  with os.fdopen(fd,'wb') as f: os.fchmod(f.fileno(),0o600); f.write(data)
  result.append(dest)
 print(json.dumps(result))
elif r['action']=='resolve':
 import shutil
 d=directory(['attachments',r['machine'],r['id']])
 if r['status']=='answered':
  fd=os.open(os.path.join(d,'answered.json'),os.O_WRONLY|os.O_CREAT|os.O_TRUNC|os.O_NOFOLLOW,0o600)
  with os.fdopen(fd,'w') as f: json.dump({'answered':r['now']},f)
  if int(time.time()*1000)-r['now']>=r['retention']: shutil.rmtree(d)
 else: shutil.rmtree(d)
elif r['action']=='cleanup':
 import shutil
 base=directory(['attachments'])
 for m in os.scandir(base):
  if not m.is_dir(follow_symlinks=False): continue
  for d in os.scandir(m.path):
   if not d.is_dir(follow_symlinks=False): continue
   try:
    fd=os.open(os.path.join(d.path,'answered.json'),os.O_RDONLY|os.O_NOFOLLOW)
    with os.fdopen(fd) as f: record=json.load(f)
    if r['now']-record['answered']>=r['retention']: shutil.rmtree(d.path)
   except FileNotFoundError: pass
`;
export class RichService {
 constructor(config) {this.config=config;this.store=new AttachmentStore(config.stateDir);this.cleanupTimer=null;this.nextCleanup=null;this.resolving=new Map();}
 async start() {await this.cleanup();}
 stop() {clearTimeout(this.cleanupTimer);}
 async settleResolutions() {await Promise.allSettled([...this.resolving.values()]);}
 scheduleCleanup(deadline) {
  if(deadline===null||this.nextCleanup!==null&&this.nextCleanup<=deadline) return;
  clearTimeout(this.cleanupTimer);this.nextCleanup=deadline;
  this.cleanupTimer=setTimeout(()=>{this.nextCleanup=null;void this.cleanup().catch(error=>process.stderr.write(`添付の整理に失敗: ${error.message}\n`));},Math.max(0,deadline-Date.now()));this.cleanupTimer.unref();
 }
 async profile(machine,signal) {
  const data=JSON.parse(String(await execute(this.config.ikesuBin,['machine','list','--json'],{signal,maxBuffer:RICH_LIMITS.pageBytes})));
  const profiles=Array.isArray(data)?data:data?.result?.machines||data?.machines||data?.result?.profiles;
  const profile=profiles?.find(p=>p.id===machine&&p.enabled);
  if(!profile?.target) throw Error('出どころマシンの SSH 設定が見つかりません');
  return profile;
 }
 async remote(machine,request,signal,maxBuffer) {
  const profile=await this.profile(machine,signal);
  return execute('/usr/bin/ssh',['-o','BatchMode=yes','-o','ConnectTimeout=15','--',profile.target,...['python3','-c',remoteScript].map(quote)],{input:JSON.stringify(request),signal,maxBuffer});
 }
 async readImage(machine,source,signal) {
  if(/^https?:/.test(source)) return (await fetchPublic(source,{signal,maxBytes:RICH_LIMITS.imageBytes})).data;
  if(!path.isAbsolute(source)) throw Error('画像の絶対パスが必要です');
  if(machine!=='local') return this.remote(machine,{action:'read',path:source,limit:RICH_LIMITS.imageBytes},signal,RICH_LIMITS.imageBytes);
  signal?.throwIfAborted();
  const file=await fs.open(source,constants.O_RDONLY|constants.O_NOFOLLOW|constants.O_NONBLOCK);
  try {
   const stat=await file.stat();if(!stat.isFile()) throw Error('通常ファイルの画像だけを表示できます');
   if(stat.size>RICH_LIMITS.imageBytes) throw Error('画像は10MBまでです');
   signal?.throwIfAborted();const data=await readBounded(file,RICH_LIMITS.imageBytes);signal?.throwIfAborted();return data;
  } finally {await file.close();}
 }
 async cache(data,extension,signal) {
  signal?.throwIfAborted();
  const dir=path.join(this.config.stateDir,'rich-cache');await fs.mkdir(dir,{recursive:true,mode:0o700});
  if((await fs.lstat(dir)).isSymbolicLink()) throw Error('画像保存先が不正です');
  const filename=path.join(dir,randomUUID()+extension);
  await fs.writeFile(filename,data,{flag:'wx',mode:0o600});
  return filename;
 }
 async load(spec,machine,signal,publish) {
  const found=discoverRichContent(spec); const cached=[];
  const image=async(source)=>{const data=await this.readImage(machine,source,signal); const file=await this.cache(data,'.image',signal);cached.push(file);return file;};
  try {
   await Promise.allSettled([
    ...found.images.map(async(source,index)=>{try {const file=await image(source);signal.throwIfAborted();publish({status:'rich_image',index,path:file,source});}catch(error){if(!signal.aborted)publish({status:'rich_error',message:`画像を表示できません: ${error.message}`});}}),
    ...found.links.map(async(url,index)=>{try {const response=await fetchPublic(url,{signal});if(!/html/i.test(response.type)) return; const metadata=await pageMetadata(response.data,response.url,signal);if(!metadata.title) return;
     let thumbnail='';if(metadata.image) {try{const data=(await fetchPublic(metadata.image,{signal,maxBytes:RICH_LIMITS.imageBytes})).data;thumbnail=await this.cache(data,'.image',signal);cached.push(thumbnail);}catch{}}
     signal.throwIfAborted();publish({status:'rich_card',index,url,title:metadata.title,site:metadata.site,path:thumbnail});
    }catch{}})
   ]);
  } finally { if(signal.aborted) await Promise.allSettled(cached.map(file=>fs.rm(file,{force:true})));else signal.addEventListener('abort',()=>{void Promise.allSettled(cached.map(file=>fs.rm(file,{force:true})));},{once:true}); }
 }
 async save(machine,id,items,signal) {
  const files=await this.store.save(machine,id,items);
  for(const file of files) await execute(this.config.nativeDialog,["--image-info",file],{signal,maxBuffer:RICH_LIMITS.pageBytes});
  if(machine==='local') return files;
  safeID(machine);safeID(id);
  const payload=await Promise.all(files.map(async file=>({extension:path.extname(file),data:(await fs.readFile(file)).toString('base64')})));
  const result=JSON.parse(String(await this.remote(machine,{action:'save',machine,id,files:payload,limit:RICH_LIMITS.attachmentBytes},signal,RICH_LIMITS.pageBytes)));
  if(!Array.isArray(result)||result.length!==files.length||result.some(p=>typeof p!=='string'||!path.isAbsolute(p))) throw Error('別マシンへの画像保存を確認できません');
  return result;
 }
 async resolve(machine,id,status,now=Date.now()) {
  safeID(machine);safeID(id);const key=`${machine}/${id}`;
  const pending=(this.resolving.get(key)||Promise.resolve()).catch(()=>{}).then(async()=>{
   const record=await this.store.queueResolution(machine,id,status,now);
   await this.store.resolve(machine,id,record.status,record.now);
   if(machine!=='local')await this.remote(machine,{action:'resolve',machine,id,status:record.status,now:record.now,retention:RICH_LIMITS.retentionMs});
   await this.store.finishResolution(machine,id);
   if(record.status==='answered')this.scheduleCleanup(record.now+RICH_LIMITS.retentionMs);
  });
  this.resolving.set(key,pending);
  try {await pending;}finally{if(this.resolving.get(key)===pending)this.resolving.delete(key);}
 }
 async retryResolutions(machine) {
  for(const record of await this.store.pendingResolutions(machine)) {
   try {await this.resolve(record.machine,record.id,record.status,record.now);}
   catch(error){process.stderr.write(`添付の整理待ちを保持: ${error.message}\n`);}
  }
 }
 async cleanup(machine='local') {
  await this.retryResolutions(machine==='local'?undefined:machine);
  if(machine!=='local') await this.remote(machine,{action:'cleanup',now:Date.now(),retention:RICH_LIMITS.retentionMs});
  const base=path.join(this.config.stateDir,'attachments');
  let machines=[];try{machines=(await fs.readdir(base,{withFileTypes:true})).filter(m=>m.isDirectory()&&!m.isSymbolicLink()&&m.name!=='local');}catch(error){if(error.code!=='ENOENT') throw error;}
  for(const item of machines) {try{await this.remote(item.name,{action:'cleanup',now:Date.now(),retention:RICH_LIMITS.retentionMs});}catch{}}
  this.scheduleCleanup(await this.store.cleanup());
 }
}
