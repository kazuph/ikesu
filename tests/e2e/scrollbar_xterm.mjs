// Replay only unmodified real PTY bytes with the local official xterm renderer.
// node scrollbar_xterm.mjs EVIDENCE XTERM_MODULES
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);
const {chromium}=require('playwright');
const evidence=path.resolve(process.argv[2]), modules=path.resolve(process.argv[3]);
const records=JSON.parse(fs.readFileSync(path.join(evidence,'pty-captures.json')));
const xterm=fs.readFileSync(path.join(modules,'@xterm/xterm/lib/xterm.js'),'utf8');
const canvas=fs.readFileSync(path.join(modules,'@xterm/addon-canvas/lib/addon-canvas.js'),'utf8');
const css=fs.readFileSync(path.join(modules,'@xterm/xterm/css/xterm.css'),'utf8');
const html=`<!doctype html><meta charset="utf-8"><title>Isolated ikesu PTY replay</title><style>${css}body{margin:0}#terminal{display:inline-block}</style><div id="terminal"></div><script>${xterm}</script><script>${canvas}</script>`;
fs.writeFileSync(path.join(evidence,'replay.html'),html);
const browser=await chromium.launch({headless:true});
const results=[];
try{
 for(const r of records){
  const page=await browser.newPage({viewport:{width:1800,height:1000},deviceScaleFactor:1});
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto('file://'+path.join(evidence,'replay.html'));
  await page.evaluate(({cols,rows,theme})=>{
   window.term=new Terminal({cols,rows,fontSize:14,fontFamily:'Menlo, monospace',lineHeight:1,customGlyphs:true,allowProposedApi:true,theme:theme==='catppuccin'?{background:'#1e1e2e',foreground:'#cdd6f4'}:{background:'#eff1f5',foreground:'#4c4f69'}});
   term.open(document.querySelector('#terminal'));term.loadAddon(new CanvasAddon.CanvasAddon());
  },r);
  const data=fs.readFileSync(r.path).toString('base64');
  await page.evaluate(data=>new Promise(resolve=>term.write(Uint8Array.from(atob(data),c=>c.charCodeAt(0)),()=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))),data);
  const state=await page.evaluate(()=>{
   const rows=Array.from({length:term.rows},(_,y)=>term.buffer.active.getLine(y)?.translateToString(false)||'');
   const cells=[];for(let y=0;y<term.rows;y++){const row=[];for(let x=0;x<term.cols;x++){const c=term.buffer.active.getLine(y)?.getCell(x);row.push({text:c?.getChars(),fg:c?.getFgColor(),bold:c?.isBold()});}cells.push(row);}
   const size=term._core._renderService.dimensions.css.cell;
   return {rows,cells,cellWidth:size.width,cellHeight:size.height,cols:term.cols,height:term.rows};
  });
  fs.writeFileSync(path.join(evidence,`${r.theme}-${r.state}-cells.json`),JSON.stringify(state));
  assert(state.rows.some(row=>row.includes('LINE-100')),`${r.theme}/${r.state}: real output absent`);
  const border=r.state==='dragged'||r.state==='copy'?33:25;
  const hovered=['hover','dragged','copy'].includes(r.state);
  for(let y=0;y<state.height;y++)assert.equal(state.cells[y][border].text,hovered?'┃':'│',`${r.theme}/${r.state} sidebar row ${y}`);
  for(let y=2;y<state.height-2;y++)assert.notEqual(state.cells[y][border+1].text,'│','second sidebar line');
  const bar=state.cells.flat().filter(c=>c.text==='▕'||c.text==='▐').length;
  assert.equal(bar>0,r.state==='scrolling'||r.state==='copy',`${r.state}: scrollbar cells ${bar}`);
  const expected=hovered? (r.theme==='catppuccin'?0x89b4fa:0x1e66f5):(r.theme==='catppuccin'?0xa6adc8:0x6c6f85);
  assert.equal(state.cells[10][border].fg,expected,`${r.state}: border color`);
  const solid=await page.evaluate(({border,expected})=>{
   const dimensions=term._core._renderService.dimensions;
   const cw=dimensions.device.cell.width,ch=dimensions.device.cell.height,h=term.rows*ch;
   let best=0;
   for(const canvas of document.querySelectorAll('.xterm-screen canvas')){
    if(canvas.height<h)continue;
    const ctx=canvas.getContext('2d');if(!ctx)continue;
    const data=ctx.getImageData(0,0,canvas.width,canvas.height).data;
    for(let x=border*cw;x<(border+1)*cw;x++){
     let matched=0;
     for(let y=0;y<h;y++){const i=(y*canvas.width+x)*4;if(data[i]===((expected>>16)&255)&&data[i+1]===((expected>>8)&255)&&data[i+2]===(expected&255)&&data[i+3]===255)matched++;}
     best=Math.max(best,matched);
    }
   }
   return {matchingRows:best,requiredRows:h};
  },{border,expected});
  assert.equal(solid.matchingRows,solid.requiredRows,`${r.theme}/${r.state}: vertical line has pixel gaps`);
  const image=path.join(evidence,`${r.theme}-${r.state}.png`);
  await page.locator('.xterm-screen').screenshot({path:image});
  assert.deepEqual(errors,[]);
  fs.writeFileSync(path.join(evidence,`${r.theme}-${r.state}-cells.json`),JSON.stringify(state));
  results.push({...r,image,scrollbarCells:bar,borderColor:expected,solid,cellWidth:state.cellWidth,cellHeight:state.cellHeight});
  await page.close();
 }
}finally{await browser.close();}
fs.writeFileSync(path.join(evidence,'checks.json'),JSON.stringify(results,null,2));
console.log(`xterm replay assertions and element screenshots: ${results.length} passed`);
