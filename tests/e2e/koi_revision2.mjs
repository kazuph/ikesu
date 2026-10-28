// Capture actual Compose() cells and PNG placements exported by the Go test.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const dest=resolve(process.argv[2]);await mkdir(dest,{recursive:true});
const frames=JSON.parse(await readFile(join(dest,'frames/frames.json'),'utf8'));
const browser=await chromium.launch({channel:'chrome',headless:true});
const results=[];
try {
 const page=await browser.newPage({viewport:{width:1280,height:720}});
 for(const frame of frames) {
  await page.goto(pathToFileURL(join(dest,'frames',frame.file)).href);
  await page.waitForFunction(()=>[...document.images].every(image=>image.complete&&image.naturalWidth>0));
  const geometry=await page.evaluate(()=>({width:document.documentElement.scrollWidth,height:document.documentElement.scrollHeight,images:[...document.images].map(image=>({x:image.offsetLeft,y:image.offsetTop,w:image.offsetWidth,h:image.offsetHeight,nativeW:image.naturalWidth,nativeH:image.naturalHeight,interpolation:getComputedStyle(image).imageRendering,z:Number(image.dataset.z)}))}));
  assert.equal(geometry.width,1280);assert.equal(geometry.height,720);
  assert.ok(geometry.images.length>=frame.scene.Fish.length);
  for(const image of geometry.images) {
   assert.equal(image.interpolation,'pixelated');assert.ok(image.x>=0&&image.y>=0&&image.x+image.w<=1280&&image.y+image.h<=720);
  }
  await page.screenshot({path:join(dest,frame.file.replace('.html','.png'))});
  results.push({time:frame.time,geometry});
 }
 // Display the committed GIF through the actual README image reference.
 const markdown=await readFile('README.md','utf8');assert.ok(markdown.includes('docs/brand/koi.gif'));
 await page.setContent(`<meta charset=utf-8><style>body{font:18px system-ui;margin:32px;background:#fff;color:#27332f}img{width:640px;image-rendering:pixelated}</style><h1>ikesu — エージェントを池で飼う</h1><img src="data:image/gif;base64,${(await readFile('docs/brand/koi.gif')).toString('base64')}">`);
 await page.waitForFunction(()=>document.images[0].complete&&document.images[0].naturalWidth>0);
 assert.equal(await page.locator('img').evaluate(image=>image.naturalHeight),320);
 await page.screenshot({path:join(dest,'readme-gif.png')});
 await writeFile(join(dest,'screenshots.json'),JSON.stringify(results,null,2));
 console.log(JSON.stringify({frames:results.length,readme:'actual committed GIF 320x320',geometry:'PASS'}));
} finally {await browser.close();}
