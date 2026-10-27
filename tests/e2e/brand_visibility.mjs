// Launch a disposable Chrome profile owned by this test. Never attaches to a
// user's browser. CDP noDefaults preserves genuine native tab visibility.
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
import {resolve,join} from 'node:path';
import {readFile,writeFile,mkdtemp,rm} from 'node:fs/promises';
import {watch} from 'node:fs';
import {tmpdir} from 'node:os';
import {spawn,execFileSync} from 'node:child_process';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const profile=await mkdtemp(join(tmpdir(),'koi-visibility-p232-'));
const activeFile=join(profile,'DevToolsActivePort');
const endpoint=new Promise((resolve,reject)=>{const timeout=setTimeout(()=>{watcher.close();reject(Error('owned Chrome did not start'));},15000);const watcher=watch(profile,async(_event,name)=>{if(name!=='DevToolsActivePort')return;try{const text=await readFile(activeFile,'utf8');const [port]=text.trim().split('\n');if(!/^\d+$/.test(port))return;clearTimeout(timeout);watcher.close();resolve('http://127.0.0.1:'+port);}catch{}});});
const chrome=spawn('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',['--user-data-dir='+profile,'--remote-debugging-port=0','--no-first-run','--no-default-browser-check','about:blank'],{stdio:'ignore'});
let browser;
try {
 browser=await chromium.connectOverCDP(await endpoint,{noDefaults:true});
 const context=browser.contexts()[0];const pond=await context.newPage();
 await pond.goto(pathToFileURL(resolve('site/index.html')).href);await pond.waitForFunction(()=>clock>0);
 const other=await context.newPage();await other.goto('about:blank');await other.bringToFront();
 await pond.waitForFunction(()=>document.hidden,null,{polling:100,timeout:5000});
 const before=await pond.evaluate(()=>({hidden:document.hidden,raf,clock,fish:JSON.stringify(fish)}));
 await other.evaluate(()=>new Promise(resolve=>setTimeout(resolve,300)));
 const after=await pond.evaluate(()=>({hidden:document.hidden,raf,clock,fish:JSON.stringify(fish)}));
 assert.deepEqual(after,before);assert.equal(after.raf,0);
 await pond.bringToFront();await pond.waitForFunction(start=>!document.hidden&&clock>start,before.clock);
 const result={before,after,resumed:await pond.evaluate(()=>clock),ownedProfile:profile};
 await writeFile(join(resolve(process.argv[2]),'visibility-results.json'),JSON.stringify(result,null,2));console.log(JSON.stringify(result));
}finally{
 if(browser)await browser.close();
 const uid=execFileSync('id',['-u'],{encoding:'utf8'}).trim();
 let owned=[];try{owned=execFileSync('pgrep',['-u',uid,'-f',profile],{encoding:'utf8'}).trim().split(/\s+/).filter(Boolean);}catch{}
 for(const pid of owned){let command;try{command=execFileSync('ps',['-ww','-p',pid,'-o','command='],{encoding:'utf8'});}catch(error){if(error.status===1)continue;throw error;}if(command.includes(profile)&&command.includes('/Applications/Google Chrome.app/'))execFileSync('kill',[pid]);}
 await rm(profile,{recursive:true,force:true});
}
