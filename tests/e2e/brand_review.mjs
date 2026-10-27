// Real Chrome checks for review regressions, including raw fish pixels below text.
import assert from 'node:assert/strict';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {mkdir,writeFile} from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const dest=resolve(process.argv[2]);await mkdir(dest,{recursive:true});
const checks=[];
for(const fallback of [true,false]){
 if(process.argv[3]&&fallback!==(process.argv[3]==='2d'))continue;
 const browser=await chromium.launch({channel:'chrome',headless:true,args:fallback?['--disable-webgl']:[]});
 try {for(const viewport of [{width:1440,height:900},{width:390,height:844},{width:390,height:620}]){
  if(process.argv[4]&&process.argv[4]!==`${viewport.width}x${viewport.height}`)continue;
  const page=await browser.newPage({viewport});await page.goto(pathToFileURL(resolve('site/index.html')).href);await page.waitForFunction(()=>atlas.complete&&atlas.naturalWidth>0&&clock>0);
  if(fallback){
   for(const point of [[.8,.6],[.85,.8],[.2,.8]]){
    await page.emulateMedia({reducedMotion:'reduce'});await page.waitForFunction(()=>raf===0);
    const x=viewport.width*point[0],y=viewport.height*point[1];await page.mouse.click(x,y);
    const mapped=await page.evaluate(()=>({x:(target.x/10+.5)*width,y:(target.y/8+.5)*height,scrollY,clock,target,fish:fish.map(f=>({...f}))}));
    checks.push({case:'fallback-input',viewport,point:[x,y],mapped,pass:Math.abs(mapped.x-x)<1&&Math.abs(mapped.y-y-mapped.scrollY)<1});
    const before=mapped.fish.reduce((sum,f)=>sum+Math.hypot(f.x-mapped.target.x,f.y-mapped.target.y),0);
    await page.emulateMedia({reducedMotion:'no-preference'});await page.waitForFunction(start=>clock>start+4,mapped.clock);
    const after=await page.evaluate(()=>fish.reduce((sum,f)=>sum+Math.hypot(f.x-target.x,f.y-target.y),0));
    checks.push({case:'fallback-approach',viewport,before,after,pass:after<before});
   }
  }else{
   for(const elapsed of [0,2,8]){
    if(elapsed)await page.waitForFunction(start=>clock>=start,elapsed);
    const pixels=await page.evaluate(()=>{
     const source=document.createElement('canvas');source.width=atlas.naturalWidth;source.height=atlas.naturalHeight;const c=source.getContext('2d');c.drawImage(atlas,0,0);const data=c.getImageData(0,0,source.width,source.height).data;const palette=new Set();for(let i=0;i<data.length;i+=4)if(data[i+3]===255)palette.add([data[i],data[i+1],data[i+2]].join(','));
     const raw=new Uint8Array(canvas.width*canvas.height*4);gl.readPixels(0,0,canvas.width,canvas.height,gl.RGBA,gl.UNSIGNED_BYTE,raw);
     const rects=[];for(const el of document.querySelectorAll('.wordmark,nav button,.eyebrow,h1,.lead,.link,footer>span')){const range=document.createRange();range.selectNodeContents(el);for(const r of range.getClientRects())rects.push({x:r.x,y:r.y+scrollY,w:r.width,h:r.height})}
     const sx=canvas.width/width,sy=canvas.height/height;let overlap=0;for(const r of rects)for(let y=Math.max(0,Math.floor(r.y*sy));y<Math.min(canvas.height,Math.ceil((r.y+r.h)*sy));y++)for(let x=Math.max(0,Math.floor(r.x*sx));x<Math.min(canvas.width,Math.ceil((r.x+r.w)*sx));x++){const i=((canvas.height-1-y)*canvas.width+x)*4;if(palette.has([raw[i],raw[i+1],raw[i+2]].join(',')))overlap++}
     const surfaces=[...document.querySelectorAll('.intro h1,.intro .lead')].map(el=>({background:getComputedStyle(el).backgroundColor,blur:getComputedStyle(el,'::before').filter}));return{clock,overlap,rects,surfaces,fish:fish.map((f,i)=>({...f,bounds:fishScreenBounds(f,i),safe:avoidsText(f,i),inside:insidePond(f)}))};
    });
    const contrast=await page.evaluate(()=>{
     const luminance=rgb=>rgb.map(v=>{v/=255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4}).reduce((sum,v,i)=>sum+v*[.2126,.7152,.0722][i],0);
     const paper=[244,240,230],original=[[37,47,43],[184,55,41],[88,104,92]];
     return ['.lead','.eyebrow','footer>span'].map((selector,i)=>{
      const el=document.querySelector(selector),scrim=getComputedStyle(el,'::before');const alpha=Number(scrim.backgroundColor.match(/[\d.]+/g).at(-1)),sigma=parseFloat(scrim.filter.match(/[\d.]+/)?.[0]||0),inset=-parseFloat(scrim.top),z=inset/sigma;
      // Gaussian blur: a union bound with Mills' inequality bounds the lost alpha.
      const lower=alpha*(1-4*Math.exp(-z*z/2)/(z*Math.sqrt(2*Math.PI)));
      const fg=getComputedStyle(el).color.match(/[\d.]+/g).slice(0,3).map(Number);
      const ratio=(luminance(paper.map(v=>v*lower))+.05)/(luminance(fg)+.05),baseline=(luminance(paper)+.05)/(luminance(original[i])+.05);
      return {selector,alphaLowerBound:lower,ratio,baseline,pass:Number.isFinite(ratio)&&ratio>=baseline};
     });
    });checks.push({case:'text-contrast',viewport,elapsed,contrast,pass:contrast.every(c=>c.pass)});
    checks.push({case:'text-avoidance',viewport,elapsed,...pixels,pass:pixels.overlap===0&&pixels.fish.every(f=>f.inside)});
    if(viewport.width<640)checks.push({case:'soft-text-background',viewport,elapsed,pass:pixels.surfaces.every(s=>s.background==='rgba(0, 0, 0, 0)'&&s.blur.includes('blur('))});
    await page.screenshot({path:join(dest,`review-${viewport.width}x${viewport.height}-${elapsed}.png`),fullPage:true});
   }
  }
  await page.close();
 }}finally{await browser.close()}
}
await writeFile(join(dest,'review-browser.json'),JSON.stringify(checks,null,2));
const failures=checks.filter(check=>!check.pass);console.log(JSON.stringify({checks:checks.length,failures},null,2));assert.equal(failures.length,0);
