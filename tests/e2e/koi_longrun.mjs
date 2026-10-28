// Stress real browser input with both real rendering backends and paused motion.
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const dest=resolve(process.argv[2]);await mkdir(dest,{recursive:true});const results=[];
for(const fallback of [false,true]){
 const browser=await chromium.launch({channel:'chrome',headless:true,args:fallback?['--disable-webgl']:[]});
 try{
  const page=await browser.newPage({viewport:{width:390,height:844},reducedMotion:'reduce'});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(pathToFileURL(resolve('site/index.html')).href);
  await page.waitForFunction(()=>atlas.complete&&atlas.naturalWidth>0);
  assert.equal(await page.evaluate(()=>!!gl),!fallback);
  const stress=await page.evaluate(()=>{
   const start=clock;
   for(let i=0;i<10000;i++)canvas.dispatchEvent(new PointerEvent('pointerdown',{clientX:10+i%300,clientY:200+i%400}));
   return {count:ripples.length,fish:fish.length,clockUnchanged:start===clock,raf};
  });
  assert.ok(stress.count<=8,JSON.stringify(stress));assert.equal(stress.fish,5);assert.equal(stress.raf,0);assert.ok(stress.clockUnchanged);
  await page.emulateMedia({reducedMotion:'no-preference'});
  await page.waitForFunction(()=>ripples.length===0);
  await page.emulateMedia({reducedMotion:'reduce'});
  const clockBefore=await page.evaluate(()=>clock);
  for(let i=0;i<100;i++)await page.setViewportSize({width:390+i%3,height:844+i%5});
  assert.equal(await page.evaluate(()=>fish.length),5);assert.equal(await page.evaluate(()=>raf),0);assert.equal(await page.evaluate(()=>clock),clockBefore);
  await page.screenshot({path:join(dest,`longrun-${fallback?'2d':'webgl'}.png`),fullPage:true});
  assert.deepEqual(errors,[]);results.push({fallback,stress,after:await page.evaluate(()=>({ripples:ripples.length,fish:fish.length,textRects:protectedText.length,raf})),errors});
 }finally{await browser.close();}
}
await writeFile(join(dest,'site-longrun.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results,null,2));
