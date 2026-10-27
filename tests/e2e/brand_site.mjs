// Run with a preinstalled Playwright module and Chrome; never downloads browsers.
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { resolve,join } from 'node:path';
import { mkdir,writeFile,readFile } from 'node:fs/promises';
const { chromium }=await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const evidence=resolve(process.argv[2]);await mkdir(evidence,{recursive:true});
const markdown=await readFile(resolve('README.md'),'utf8');
const gif=(await readFile(resolve('docs/brand/koi.gif'))).toString('base64');
const escape=text=>text.replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('>','&gt;').replaceAll('\"','&quot;');
const inline=text=>escape(text).replace(/`([^`]+)`/g,'<code>$1</code>').replace(/\[([^\]]+)\]\(([^)]+)\)/g,(_all,label,url)=>`<a href="${new URL(url,pathToFileURL(resolve('README.md'))).href}">${label}</a>`);
let code=false;const rendered=[];
for(const line of markdown.split('\n')){if(line.startsWith('```')){rendered.push(code?'</pre>':'<pre>');code=!code;}else if(code){rendered.push(escape(line)+'\n');}else if(line.startsWith('![')){rendered.push(`<img alt="${escape(line.slice(2,line.indexOf(']')))}" src="data:image/gif;base64,${gif}">`);}else if(line.startsWith('# ')){rendered.push('<h1>'+inline(line.slice(2))+'</h1>');}else if(line.startsWith('## ')){rendered.push('<h2>'+inline(line.slice(3))+'</h2>');}else if(line==='---'){rendered.push('<hr>');}else if(line){rendered.push('<p>'+inline(line)+'</p>');}}
await writeFile(join(evidence,'README-preview.html'),'<!doctype html><html lang="ja"><meta charset="utf-8"><title>README preview</title><style>body{max-width:900px;margin:40px auto;padding:24px;color:#27332f;font:16px/1.8 system-ui}img{width:640px;max-width:100%;image-rendering:pixelated}pre{padding:16px;background:#f4f4f4;white-space:pre-wrap}h1{font-size:30px}a{color:#a32f23}hr{margin:40px 0}</style><body>'+rendered.join('')+'</body></html>');
const browser=await chromium.launch({channel:'chrome',headless:true});
const results=[];
try {
 for(const viewport of [{width:1440,height:1000},{width:390,height:844},{width:390,height:620}]) {
  const page=await browser.newPage({viewport});const errors=[],requests=[];
  page.on('pageerror',error=>errors.push(error.message));page.on('request',request=>{if(/^https?:/.test(request.url()))requests.push(request.url())});
  await page.goto(pathToFileURL(resolve('site/index.html')).href);
  await page.waitForFunction(()=>document.querySelector('canvas').width>0&&atlas.complete&&atlas.naturalWidth>0&&clock>0);
  assert.equal(await page.locator('h1').innerText(),'エージェントを\n池で飼う。');
  const geometry=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,main:document.querySelector('main').getBoundingClientRect().toJSON(),heading:document.querySelector('h1').getBoundingClientRect().toJSON()}));
  assert.equal(geometry.scroll,geometry.width);assert.ok(geometry.heading.x>=0&&geometry.heading.right<=viewport.width);
  await page.screenshot({path:join(evidence,`site-${viewport.width}-ja.png`),fullPage:true});
  const scattered=[];
  for(let sample=0;sample<3;sample++){
   if(sample)await page.waitForFunction(start=>clock>=start+2,await page.evaluate(()=>clock));
   const state=await page.evaluate(()=>({clock,fish:fish.map(f=>({...f})),textSurfaces:[...document.querySelectorAll('.intro h1,.intro .eyebrow,.intro .lead,.intro .link')].map(el=>getComputedStyle(el).backgroundColor)}));
   const xs=state.fish.map(f=>f.x);assert.ok(Math.max(...xs)-Math.min(...xs)>1.6,'fish collapsed into a vertical sample column');
   for(let i=0;i<state.fish.length;i++)for(let j=i+1;j<state.fish.length;j++)assert.ok(Math.hypot(state.fish[i].x-state.fish[j].x,state.fish[i].y-state.fish[j].y)>=Math.hypot(1.6,.64)+.04,'rotated sprite bounds overlap');
   if(viewport.width<640)assert.ok(state.textSurfaces.every(color=>color==='rgb(244, 240, 230)'),'mobile text must have an opaque surface');
   scattered.push(state);await page.screenshot({path:join(evidence,`site-${viewport.width}-ja-time-${sample}.png`),fullPage:true});
  }
  await writeFile(join(evidence,`scatter-${viewport.width}-${viewport.height}.json`),JSON.stringify(scattered,null,2));

  const webgl=await page.evaluate(()=>({renderer:gl?'webgl2':'2d',error:gl?gl.getError():null,atlas:[atlas.naturalWidth,atlas.naturalHeight],footer:document.querySelector('footer').getBoundingClientRect().toJSON(),height:document.documentElement.scrollHeight}));
  assert.equal(webgl.renderer,'webgl2');assert.equal(webgl.error,0);assert.deepEqual(webgl.atlas,[320,160]);assert.ok(webgl.footer.bottom<=webgl.height,JSON.stringify(webgl));

  await page.getByRole('button',{name:'English',exact:true}).click();
  assert.equal(await page.locator('html').getAttribute('lang'),'en');assert.match(await page.locator('h1').innerText(),/Keep your agents/);
  await page.screenshot({path:join(evidence,`site-${viewport.width}-en.png`),fullPage:true});
  await page.emulateMedia({reducedMotion:'reduce'});await page.waitForFunction(()=>raf===0);
  const waterBefore=await page.evaluate(()=>{const pixels=new Uint8Array(canvas.width*canvas.height*4);gl.readPixels(0,0,canvas.width,canvas.height,gl.RGBA,gl.UNSIGNED_BYTE,pixels);window.brandProofPixels=pixels;const source=document.createElement('canvas');source.width=atlas.naturalWidth;source.height=atlas.naturalHeight;const sample=source.getContext('2d');sample.drawImage(atlas,0,0);const rgba=sample.getImageData(0,0,source.width,source.height).data,palette=new Set();for(let i=0;i<rgba.length;i+=4)if(rgba[i+3]===255)palette.add([rgba[i],rgba[i+1],rgba[i+2]].join(','));let fishPixels=0;for(let i=0;i<pixels.length;i+=4)if(palette.has([pixels[i],pixels[i+1],pixels[i+2]].join(',')))fishPixels++;return{clock,fishPixels,nearest:gl.getTexParameter(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER)===gl.NEAREST};});
  assert.ok(waterBefore.fishPixels>0);assert.equal(waterBefore.nearest,true);
  await page.mouse.click(viewport.width*.85,viewport.height*.75);
  const waterAfter=await page.evaluate(()=>{const pixels=new Uint8Array(canvas.width*canvas.height*4);gl.readPixels(0,0,canvas.width,canvas.height,gl.RGBA,gl.UNSIGNED_BYTE,pixels);let changed=0;for(let i=0;i<pixels.length;i+=4)if(pixels[i]!==brandProofPixels[i]||pixels[i+1]!==brandProofPixels[i+1]||pixels[i+2]!==brandProofPixels[i+2])changed++;delete window.brandProofPixels;return{clock,changedPixels:changed};});
  assert.equal(waterBefore.clock,waterAfter.clock);assert.ok(waterAfter.changedPixels>0);

  await page.screenshot({path:join(evidence,`site-${viewport.width}-ripple.png`),fullPage:true});
  await page.emulateMedia({reducedMotion:'no-preference'});
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
  results.push({viewport,geometry,waterBefore,waterAfter,approach:{initialDistance,finalDistance},renderer:await page.evaluate(()=>gl?'webgl2':'2d'),reducedMotion:'pixels, positions and clock unchanged',errors,externalRequests:requests});
  await page.close();
 }
 const readme=await browser.newPage({viewport:{width:1200,height:1000}});
 await readme.goto(pathToFileURL(join(evidence,'README-preview.html')).href);
 await readme.locator('img').waitFor({state:'visible'});assert.equal(await readme.locator('h1').count(),2);
 await readme.screenshot({path:join(evidence,'readme.png'),fullPage:true});
 await writeFile(join(evidence,'browser-results.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results,null,2));
} finally {await browser.close();}
