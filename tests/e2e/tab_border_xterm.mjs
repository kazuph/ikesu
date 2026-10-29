// Replay unmodified isolated PTY bytes; measure tab fills and every pane edge.
// Stroke continuity distinguishes ink from the exact terminal background. Canvas
// intersections can round an 8-bit color channel by one, without creating a gap.
import fs from 'node:fs';import path from 'node:path';import assert from 'node:assert/strict';import {createRequire} from 'node:module';
const require=createRequire(import.meta.url),{chromium}=require('playwright');
const root=path.resolve(process.argv[2]),modules=path.resolve(process.argv[3]),strict=process.argv.includes('--strict');
const xterm=fs.readFileSync(path.join(modules,'@xterm/xterm/lib/xterm.js'),'utf8'),addon=fs.readFileSync(path.join(modules,'@xterm/addon-canvas/lib/addon-canvas.js'),'utf8'),css=fs.readFileSync(path.join(modules,'@xterm/xterm/css/xterm.css'),'utf8');
const themes={'catppuccin':['#1e1e2e','#cdd6f4',0x89b4fa],'catppuccin-latte':['#eff1f5','#4c4f69',0x1e66f5],'tokyo-night':['#1a1b26','#c0caf5',0x7aa2f7],'tokyo-night-day':['#e1e2e7','#3760bf',0x2e7de9]};
const html=`<!doctype html><meta charset="utf-8"><style>${css}body{margin:0}#terminal{display:inline-block}</style><div id="terminal"></div><script>${xterm}</script><script>${addon}</script>`;
fs.writeFileSync(path.join(root,'replay.html'),html);
const browser=await chromium.launch({headless:true}),results=[];
try{for(const dir of fs.readdirSync(root).sort()){
 const folder=path.join(root,dir),manifest=path.join(folder,'captures.json');if(!fs.existsSync(manifest))continue;
 const captures=JSON.parse(fs.readFileSync(manifest));
 for(const r of captures.records){
  const page=await browser.newPage({viewport:{width:1800,height:1000},deviceScaleFactor:1});const errors=[];page.on('pageerror',e=>errors.push(e.message));await page.goto('file://'+path.join(root,'replay.html'));
  await page.evaluate(({r,theme})=>{window.term=new Terminal({cols:r.cols,rows:r.rows,fontSize:14,fontFamily:'Menlo, monospace',lineHeight:1,customGlyphs:true,allowProposedApi:true,theme:{background:theme[0],foreground:theme[1]}});term.open(document.querySelector('#terminal'));term.loadAddon(new CanvasAddon.CanvasAddon());},{r,theme:themes[r.theme]});
  await page.evaluate(data=>new Promise(resolve=>term.write(Uint8Array.from(atob(data),c=>c.charCodeAt(0)),()=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))),fs.readFileSync(path.join(folder,path.basename(r.path))).toString('base64'));
  const state=await page.evaluate(()=>{const cells=[];for(let y=0;y<term.rows;y++){const row=[];for(let x=0;x<term.cols;x++){const c=term.buffer.active.getLine(y).getCell(x);row.push({text:c.getChars(),fg:c.getFgColor(),bg:c.getBgColor(),bgDefault:c.isBgDefault(),underline:c.isUnderline()});}cells.push(row);}return{cells,rows:cells.map(row=>row.map(c=>c.text||' ').join('')),cw:term._core._renderService.dimensions.device.cell.width,ch:term._core._renderService.dimensions.device.cell.height};});
  fs.writeFileSync(path.join(folder,r.active+'-cells.json'),JSON.stringify(state));
  assert(state.rows[0].includes('boss')&&state.rows[0].includes('impl'),'tab labels absent');assert(state.rows.some(s=>s.includes('BORDER_READY')),'real shell output absent');
  const selected=[],inactive=[],tabPixels=[];
  for(const label of ['boss','impl']){const x=state.rows[0].indexOf(label);for(let n=0;n<label.length;n++)(label===r.active?selected:inactive).push(state.cells[0][x+n]);}
  for(const label of ['boss','impl']){
   const expected=label===r.active?themes[r.theme][2]:parseInt((captures.theme_custom.surface0||({'catppuccin':'#313244','catppuccin-latte':'#ccd0da','tokyo-night':'#24283b','tokyo-night-day':'#c4c8da'}[r.theme])).slice(1),16);
   const x=state.rows[0].indexOf(label)-1;
   const pixels=await page.evaluate(({x,expected})=>{const d=term._core._renderService.dimensions.device.cell;let best=0;for(const canvas of document.querySelectorAll('.xterm-screen canvas')){const ctx=canvas.getContext('2d');if(!ctx)continue;const image=ctx.getImageData(x*d.width,0,d.width,d.height).data;let count=0;for(let i=0;i<image.length;i+=4)if(image[i]===(expected>>16&255)&&image[i+1]===(expected>>8&255)&&image[i+2]===(expected&255)&&image[i+3]===255)count++;best=Math.max(best,count);}return{matched:best,required:d.width*d.height};},{x,expected});
   tabPixels.push({label,selected:label===r.active,expected,pixels});
  }
  const tab={selectedFill:selected.every(c=>!c.bgDefault&&c.bg===themes[r.theme][2]),selectedUnderline:selected.some(c=>c.underline),inactiveAccent:inactive.some(c=>!c.bgDefault&&c.bg===themes[r.theme][2]),selectedBackgrounds:[...new Set(selected.map(c=>c.bgDefault?'default':c.bg))],inactiveBackgrounds:[...new Set(inactive.map(c=>c.bgDefault?'default':c.bg))]};
  const edges=[];for(const pane of r.layout.panes){const b=pane.rect;for(const side of ['left','right']){
   const originalX=side==='left'?b.x:b.x+b.width-1;
   const sharedX=side==='left'&&originalX===r.layout.area.x&&originalX>0?originalX-1:side==='right'&&originalX===r.layout.area.x+r.layout.area.width-1&&originalX<r.cols-1?originalX+1:originalX;
   for(const [kind,x] of [['pane',originalX],['shared',sharedX]]){
    const missing=[];for(let y=b.y+1;y<b.y+b.height-1;y++)if(!['│','┃'].includes(state.cells[y][x].text))missing.push(y);
    const joinX=side==='left'?x+1:x-1;
    const joined=kind!=='shared'||sharedX===originalX||[b.y,b.y+b.height-1].every(y=>state.cells[y][joinX].text==='─');
    const pixels=await page.evaluate(({x,y,h,cw,ch,bg})=>{
     const top=y*ch,bottom=(y+h)*ch,innerTop=top+ch,innerBottom=bottom-ch;let best={matched:0,required:(h-2)*ch,innerMatched:0,continuous:false,cornerRows:false};
     for(const canvas of document.querySelectorAll('.xterm-screen canvas')){if(canvas.height<bottom)continue;const ctx=canvas.getContext('2d');if(!ctx)continue;const image=ctx.getImageData(0,0,canvas.width,canvas.height).data;
      for(let xx=x*cw;xx<(x+1)*cw;xx++){let matched=0,innerMatched=0,first=-1,last=-1;for(let yy=top;yy<bottom;yy++){const c=term.buffer.active.getLine(Math.floor(yy/ch)).getCell(x),fg=c.getFgColor(),i=(yy*canvas.width+xx)*4;if(fg>=0&&image[i+3]>0&&(image[i]!==bg[0]||image[i+1]!==bg[1]||image[i+2]!==bg[2])){matched++;if(yy>=innerTop&&yy<innerBottom)innerMatched++;if(first<0)first=yy;last=yy;}}if(innerMatched>best.innerMatched||innerMatched===best.innerMatched&&matched>best.matched)best={matched,required:(h-2)*ch,innerMatched,continuous:matched===last-first+1,cornerRows:first<innerTop&&last>=innerBottom,first,last};}}
     return best;
    },{x,y:b.y,h:b.height,cw:state.cw,ch:state.ch,bg:themes[r.theme][0].slice(1).match(/../g).map(x=>parseInt(x,16))});
    edges.push({pane:pane.pane_id,side,kind,x,joined,missingRows:missing,pixels});
   }
  }}
  const image=path.join(folder,r.active+'.png');await page.locator('.xterm-screen').screenshot({path:image});fs.writeFileSync(path.join(folder,r.active+'-cells.json'),JSON.stringify(state));
  if(strict){assert(tab.selectedFill,`${dir}/${r.active}: selected fill lost`);assert(!tab.selectedUnderline,'selected tab became underline');assert(!tab.inactiveAccent,'inactive tab has selected fill');for(const chip of tabPixels)assert.equal(chip.pixels.matched,chip.pixels.required,`${dir}/${r.active}/${chip.label}: tab fill pixels`);for(const edge of edges.filter(e=>e.kind==='shared')){assert(edge.joined,`${dir}/${r.active}/${edge.pane}/${edge.side}: detached corner`);assert.deepEqual(edge.missingRows,[],`${dir}/${r.active}/${edge.pane}/${edge.side}: missing rows`);assert(edge.pixels.continuous&&edge.pixels.cornerRows,`${dir}/${r.active}/${edge.pane}/${edge.side}: edge endpoints/gaps`);assert.equal(edge.pixels.innerMatched,edge.pixels.required,`${dir}/${r.active}/${edge.pane}/${edge.side}: pixel gaps`);}}
  assert.deepEqual(errors,[]);results.push({scene:dir,active:r.active,theme:r.theme,image,tab,tabPixels,edges});await page.close();
 }
}}finally{await browser.close();fs.writeFileSync(path.join(root,'checks.json'),JSON.stringify(results,null,2));}
console.log(JSON.stringify(results.map(r=>({scene:r.scene,active:r.active,tab:r.tab,missingPaneRows:r.edges.filter(e=>e.kind==='pane').reduce((n,e)=>n+e.missingRows.length,0),missingSharedRows:r.edges.filter(e=>e.kind==='shared').reduce((n,e)=>n+e.missingRows.length,0)})),null,2));
