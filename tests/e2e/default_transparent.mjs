// Real PTY streams rendered by xterm.js in an isolated Playwright browser.
import assert from 'node:assert/strict';
import {readFile,writeFile,mkdir} from 'node:fs/promises';
import {resolve,join} from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const root=resolve(process.argv[2]);
const assets=resolve(process.env.XTERM_ASSETS);
const canvasAddon=resolve(process.env.XTERM_CANVAS_ADDON);
await mkdir(root,{recursive:true});
const html=String.raw`<!doctype html><meta charset="utf-8"><title>Isolated ikesu background comparison</title>
<link rel="stylesheet" href="${pathToFileURL(join(assets,'css/xterm.css')).href}">
<style>body{margin:0;padding:20px;font:14px system-ui;background:#101a26;color:#cdd6f4}h1{font-size:18px;margin:0 0 16px}main{display:flex;gap:20px}section{flex:none}h2{font-size:14px}.terminal{padding:12px;background:transparent;border:1px solid #737aa2}.xterm-viewport{background:transparent!important}</style>
<h1>Actual ikesu output: before / after (temporary HOME)</h1><main><section><h2>Before</h2><div class="terminal" id="before"></div></section><section><h2>After</h2><div class="terminal" id="after"></div></section></main>
<script src="${pathToFileURL(join(assets,'lib/xterm.js')).href}"></script><script src="${pathToFileURL(join(canvasAddon,'lib/addon-canvas.js')).href}"></script>
<script>window.terminals={};for(const id of ['before','after']){const term=new Terminal({cols:120,rows:40,fontSize:13,fontFamily:'monospace',allowTransparency:true,theme:{background:'#00000000'}});term.open(document.getElementById(id));term.loadAddon(new CanvasAddon.CanvasAddon());window.terminals[id]=term;}window.render=(id,data)=>new Promise(resolve=>terminals[id].write(Uint8Array.from(atob(data),c=>c.charCodeAt(0)),resolve));</script>`;
const fixture=join(root,'comparison.html');await writeFile(fixture,html);
const browser=await chromium.launch({headless:true});const results=[];const browserErrors=[];
try{
 for(const theme of ['default','catppuccin-latte'])for(const background of ['default','opaque','custom']){
  const page=await browser.newPage({viewport:{width:2140,height:760},deviceScaleFactor:1});
  page.on('pageerror',error=>browserErrors.push(String(error)));
  await page.goto(pathToFileURL(fixture).href,{waitUntil:'networkidle'});
  await page.locator('h1').evaluate((el,label)=>{el.textContent='Actual ikesu output: before / after — '+label},background);
  await page.waitForFunction(()=>!!window.terminals?.after);
  if(theme==='catppuccin-latte')await page.evaluate(()=>{document.body.style.background='#fff8e7';document.body.style.color='#4c4f69';for(const term of Object.values(terminals))term.options.theme={background:'#00000000',foreground:'#4c4f69'}});
  for(const variant of ['before','after']){
   const name=variant==='after'&&background!=='default'?`${theme}-after-${background}`:`${theme}-${variant}`;
   const capture=JSON.parse(await readFile(join(root,name,'capture.json'),'utf8'));
   if(theme==='default'&&background==='default')assert.equal(capture.config_present_at_start,false,'default capture started with configuration');
   const data=await readFile(join(root,name,'screen.ansi'));
   await page.evaluate(([id,base64])=>window.render(id,base64),[variant,data.toString('base64')]);
  }
  const measurements=await page.evaluate(()=>Object.fromEntries(Object.entries(terminals).map(([id,term])=>{
   const cells=[];let nonDefaultBackground=0;const lines=[];
   for(let y=0;y<term.rows;y++){const line=term.buffer.active.getLine(y);lines.push(line.translateToString());for(let x=0;x<term.cols;x++){const c=line.getCell(x);const bg=c.getBgColorMode();if(bg!==0)nonDefaultBackground++;cells.push({x,y,text:c.getChars(),backgroundMode:bg,background:c.getBgColor(),foregroundMode:c.getFgColorMode(),foreground:c.getFgColor()})}}
   return [id,{nonDefaultBackground,lines,cells}];
  })));
  for(const variant of ['before','after'])assert(measurements[variant].lines.join('\n').includes('TRANSPARENCY_READY'),`${theme}/${variant}: real PTY marker missing`);
  assert(measurements.before.nonDefaultBackground>0,'before did not exercise painted chrome');
  if(background==='default')assert.equal(measurements.after.nonDefaultBackground,0,`${theme}: default background remained painted`);
  else {assert.equal(measurements.after.nonDefaultBackground,4800,`${theme}/${background}: some regions were not filled`);const expected=background==='custom'?0x2a4639:theme==='default'?0x181825:0xeff1f5;assert(measurements.after.cells.every(c=>c.background===expected),`${theme}/${background}: backgrounds differ by region`);}
  const geometry=await page.evaluate(()=>({width:innerWidth,scrollWidth:document.documentElement.scrollWidth,rects:[...document.querySelectorAll('section > .terminal')].map(el=>{const r=el.getBoundingClientRect();return{x:r.x,y:r.y,width:r.width,height:r.height,right:r.right,bottom:r.bottom}})}));
  assert(geometry.scrollWidth<=geometry.width,'comparison layout overflowed');
  assert(geometry.rects[0].right<=geometry.rects[1].x,'terminal comparisons overlap');
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  await page.screenshot({path:join(root,`${theme}-${background}-before-after.png`),fullPage:true});
  await writeFile(join(root,`${theme}-${background}-cells.json`),JSON.stringify(measurements,null,2));
  results.push({theme,background,geometry,before:measurements.before.nonDefaultBackground,after:measurements.after.nonDefaultBackground,markerVerified:true});
  await page.close();
 }
}finally{await browser.close()}
assert.deepEqual(browserErrors,[]);
await writeFile(join(root,'browser-results.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(results));
