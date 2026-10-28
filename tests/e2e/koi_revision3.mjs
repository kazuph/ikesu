// Real WebGL2 and native 2D fallback timing; uses preinstalled Chrome only.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const dest=resolve(process.argv[2]);await mkdir(dest,{recursive:true});const results=[];
for(const fallback of [false,true]){
 const browser=await chromium.launch({channel:'chrome',headless:true,args:fallback?['--disable-webgl']:[]});
 try{for(const viewport of [{width:1440,height:900},{width:390,height:844}]){
 const page=await browser.newPage({viewport});await page.goto(pathToFileURL(resolve('site/index.html')).href);
 await page.waitForFunction(()=>atlas.complete&&atlas.naturalWidth&&clock>0);
 assert.equal(await page.evaluate(()=>!!gl),!fallback);
 await page.mouse.click(viewport.width*.85,viewport.height*.75);
 const before=await page.evaluate(()=>({clock,start:target.start,rippleLifetime,gatherDuration,ripples:ripples.length,distances:fish.map(f=>Math.hypot(f.x-target.x,f.y-target.y))}));
 assert.equal(before.rippleLifetime,.8);assert.equal(before.gatherDuration,1.5);assert.equal(before.ripples,1);
 await page.waitForFunction(start=>clock>start+.4,before.start);
 await page.screenshot({path:join(dest,`fast-ripple-${fallback?'2d':'3d'}-${viewport.width}.png`),fullPage:true});
 await page.waitForFunction(start=>clock>start+rippleLifetime,before.start);
 assert.equal(await page.evaluate(()=>ripples.length),0);
 await page.waitForFunction(start=>clock>=start+gatherDuration,before.start);
 const after=await page.evaluate(()=>({clock,ripples:ripples.length,distances:fish.map(f=>Math.hypot(f.x-target.x,f.y-target.y)),speeds:fish.map(f=>f.speed),caps:fish.map(f=>.6*(1-f.variety/(fish.length*2))*gatherSpeedLimit)}));
 assert.ok(after.distances.reduce((a,b)=>a+b,0)<before.distances.reduce((a,b)=>a+b,0));
 after.speeds.forEach((speed,i)=>assert.ok(speed<=after.caps[i]+1e-9));
 await page.screenshot({path:join(dest,`gather-${fallback?'2d':'3d'}-${viewport.width}.png`),fullPage:true});
 results.push({viewport,renderer:fallback?'2d':'3d',before,after});await page.close();
 }}finally{await browser.close();}
}
await writeFile(join(dest,'fast-timing.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results));
