// Run with a preinstalled Playwright module and Chrome; never downloads browsers.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { resolve,join } from 'node:path';
import { mkdir,writeFile } from 'node:fs/promises';
const { chromium }=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const evidence=resolve(process.argv[2]);await mkdir(evidence,{recursive:true});
const browser=await chromium.launch({channel:'chrome',headless:true});
const results=[];
try {
 for(const viewport of [{width:1440,height:1000},{width:390,height:844}]) {
  const page=await browser.newPage({viewport});const errors=[],requests=[];
  page.on('pageerror',error=>errors.push(error.message));page.on('request',request=>{if(/^https?:/.test(request.url()))requests.push(request.url())});
  await page.goto(pathToFileURL(resolve('site/index.html')).href);
  await page.waitForFunction(()=>document.querySelector('canvas').width>0&&atlas.complete&&atlas.naturalWidth>0&&clock>0);
  assert.equal(await page.locator('h1').innerText(),'エージェントを\n池で飼う。');
  const geometry=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,main:document.querySelector('main').getBoundingClientRect().toJSON(),heading:document.querySelector('h1').getBoundingClientRect().toJSON()}));
  assert.equal(geometry.scroll,geometry.width);assert.ok(geometry.heading.x>=0&&geometry.heading.right<=viewport.width);
  await page.screenshot({path:join(evidence,`site-${viewport.width}-ja.png`),fullPage:true});
  const webgl=await page.evaluate(()=>({renderer:gl?'webgl2':'2d',error:gl?gl.getError():null,atlas:[atlas.naturalWidth,atlas.naturalHeight],footer:document.querySelector('footer').getBoundingClientRect().toJSON(),height:document.documentElement.scrollHeight}));
  assert.equal(webgl.renderer,'webgl2');assert.equal(webgl.error,0);assert.deepEqual(webgl.atlas,[320,160]);assert.ok(webgl.footer.bottom<=webgl.height);

  await page.getByRole('button',{name:'English',exact:true}).click();
  assert.equal(await page.locator('html').getAttribute('lang'),'en');assert.match(await page.locator('h1').innerText(),/Keep your agents/);
  await page.screenshot({path:join(evidence,`site-${viewport.width}-en.png`),fullPage:true});
  await page.mouse.click(viewport.width*.85,viewport.height*.75);
  await page.screenshot({path:join(evidence,`site-${viewport.width}-ripple.png`),fullPage:true});
  const clicked=await page.evaluate(()=>({ripples:ripples.length,target,fish:fish.map(f=>({x:f.x,y:f.y})),clock}));
  assert.equal(clicked.ripples,1);assert.ok(clicked.target);
  const initialDistance=clicked.fish.reduce((s,f)=>s+Math.hypot(f.x-clicked.target.x,f.y-clicked.target.y),0);
  await page.waitForFunction(start=>clock>start+4,clicked.clock);
  const finalDistance=await page.evaluate(()=>fish.reduce((s,f)=>s+Math.hypot(f.x-target.x,f.y-target.y),0));
  assert.ok(finalDistance<initialDistance,`koi did not approach: ${initialDistance} -> ${finalDistance}`);
  await page.emulateMedia({reducedMotion:'reduce'});
  await page.waitForFunction(()=>raf===0);
  const frozen=await page.evaluate(()=>({clock,fish:JSON.stringify(fish),pixels:canvas.toDataURL()}));
  // Real elapsed time comes from a browser timer, without replacing any clock.
  await page.evaluate(()=>new Promise(resolve=>setTimeout(resolve,300)));
  const later=await page.evaluate(()=>({clock,fish:JSON.stringify(fish),pixels:canvas.toDataURL()}));
  assert.deepEqual(later,frozen);
  assert.deepEqual(errors,[]);assert.deepEqual(requests,[]);
  results.push({viewport,geometry,approach:{initialDistance,finalDistance},renderer:await page.evaluate(()=>gl?'webgl2':'2d'),reducedMotion:'pixels, positions and clock unchanged',errors,externalRequests:requests});
  await page.close();
 }
 const readme=await browser.newPage({viewport:{width:1200,height:1000}});
 await readme.goto(pathToFileURL(join(evidence,'README-preview.html')).href);
 await readme.locator('img').waitFor({state:'visible'});assert.equal(await readme.locator('h1').count(),2);
 await readme.screenshot({path:join(evidence,'readme.png'),fullPage:true});
 await writeFile(join(evidence,'browser-results.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results,null,2));
} finally {await browser.close();}
