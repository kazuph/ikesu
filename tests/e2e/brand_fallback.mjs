import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
import {resolve,join} from 'node:path';
import {writeFile} from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({channel:'chrome',headless:true,args:['--disable-webgl']});
try {
 const page=await browser.newPage({viewport:{width:390,height:844}});
 await page.goto(pathToFileURL(resolve('site/index.html')).href);await page.waitForFunction(()=>atlas.complete&&atlas.naturalWidth>0&&clock>0);
 await page.emulateMedia({reducedMotion:'reduce'});await page.waitForFunction(()=>raf===0);
 const proof=await page.evaluate(()=>{
  const pixels=ctx.getImageData(0,0,canvas.width,canvas.height).data,colors=new Set();
  for(let i=0;i<pixels.length;i+=4)colors.add(pixels.slice(i,i+4).join(','));
  return {webgl:gl!==null,smoothing:ctx.imageSmoothingEnabled,colorCount:colors.size,clock,canvas:[canvas.width,canvas.height]};
 });
 assert.equal(proof.webgl,false);assert.equal(proof.smoothing,false);assert.ok(proof.colorCount<=41,`interpolated colors: ${proof.colorCount}`);
 await page.screenshot({path:join(resolve(process.argv[2]),'site-fallback-390.png'),fullPage:true});
 await writeFile(join(resolve(process.argv[2]),'fallback-results.json'),JSON.stringify(proof,null,2));console.log(JSON.stringify(proof));
}finally{await browser.close();}
