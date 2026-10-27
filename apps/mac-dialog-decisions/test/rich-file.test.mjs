import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const exec=promisify(execFile);
const fifoDeadlineMs=2000;
// The job's 120-second bound also covers process startup and module loading.
const processDeadlineMs=120_000;
function runFIFOProbe(script) {
 return new Promise((resolve,reject)=>{
  let timer,output='';
  const started=Date.now();
  const child=execFile(process.execPath,['--input-type=module','-e',script],{timeout:processDeadlineMs},(error,stdout,stderr)=>{
   clearTimeout(timer);
   if(error) {error.stdout=stdout;error.stderr=stderr;error.message+=`\nFIFO phases: ${stdout}\n${stderr}`;reject(error);}
   else resolve({stdout,startupMs:Number(stdout.match(/FIFO_READY (\d+)/)?.[1])-started});
  });
  child.stdout.on('data',data=>{
   output+=data;
   if(!timer&&output.includes('FIFO_READY ')) timer=setTimeout(()=>child.kill('SIGTERM'),fifoDeadlineMs);
   if(output.includes('FIFO_REJECTED ')) clearTimeout(timer);
  });
 });
}
for(const operation of ['preview','attachment']) test(`actual unwritten FIFO is rejected without blocking ${operation}`,async t=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-fifo-'));
 try {
  const draft=path.join(root,'drafts/local/fifo');await fs.mkdir(draft,{recursive:true});const fifo=path.join(draft,'画像.png');await exec('/usr/bin/mkfifo',[fifo]);
  const service=new URL('../rich-service.mjs',import.meta.url).href;
  const script=`const {RichService}=await import(${JSON.stringify(service)}); const s=new RichService({stateDir:${JSON.stringify(root)}});console.log('FIFO_READY',Date.now());const began=performance.now();try {${operation==='preview'?`await s.readImage('local',${JSON.stringify(fifo)},new AbortController().signal)`:`await s.store.save('local','fifo',[{path:${JSON.stringify(fifo)}}])`};process.exitCode=1;}catch(error){if(!/通常|regular/.test(error.message))throw error;console.log('FIFO_REJECTED',performance.now()-began);}`;
  const result=await runFIFOProbe(script);assert.match(result.stdout,/FIFO_REJECTED/);
  const elapsed=Number(result.stdout.match(/FIFO_REJECTED ([\d.]+)/)[1]);assert.ok(elapsed<fifoDeadlineMs,`FIFO rejection took ${elapsed}ms`);t.diagnostic(`startup=${result.startupMs}ms FIFO=${elapsed}ms`);
 } finally {await fs.rm(root,{recursive:true,force:true});}
});

test('FIFO probe still terminates an actual blocking open after readiness',async()=>{
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-blocking-fifo-'));
 try {
  const fifo=path.join(root,'unwritten');await exec('/usr/bin/mkfifo',[fifo]);
  await assert.rejects(runFIFOProbe(`import fs from 'node:fs';console.log('FIFO_READY',Date.now());fs.openSync(${JSON.stringify(fifo)},fs.constants.O_RDONLY);`),error=>error.signal==='SIGTERM'&&error.stdout.includes('FIFO_READY')&&!error.stdout.includes('FIFO_REJECTED'));
 } finally {await fs.rm(root,{recursive:true,force:true});}
});

test('native image validation rejects an unwritten FIFO without blocking',async()=>{
 const binary=process.env.MAC_DIALOG_TEST_NATIVE_BINARY;assert.ok(binary,'actual native binary required');
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-native-fifo-'));
 try {
  const fifo=path.join(root,'画像.png');await exec('/usr/bin/mkfifo',[fifo]);
  await assert.rejects(exec(binary,['--image-info',fifo],{timeout:2000}),error=>error.code===1&&!error.signal);
 } finally {await fs.rm(root,{recursive:true,force:true});}
});
