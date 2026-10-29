import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import assert from 'node:assert/strict';
const require=createRequire(import.meta.url),{chromium}=require('playwright');
const root=path.resolve(process.argv[2]);
const records=JSON.parse(fs.readFileSync(path.join(root,'checks.json')));
const browser=await chromium.launch({headless:true});
const page=await browser.newPage();
const results=[];
try{
 for(const theme of ['catppuccin','catppuccin-latte']){
  const pair=['no-hover','hover'].map(state=>records.find(r=>r.theme===theme&&r.state===state));
  const input=pair.map(r=>({state:r.state,fg:r.borderColor,cw:r.cellWidth,png:fs.readFileSync(r.image).toString('base64')}));
  const measured=await page.evaluate(async input=>{
   const images=[];
   for(const r of input){
    const image=new Image();image.src='data:image/png;base64,'+r.png;await image.decode();
    const canvas=document.createElement('canvas');canvas.width=image.width;canvas.height=image.height;
    const ctx=canvas.getContext('2d');ctx.drawImage(image,0,0);
    const pixels=ctx.getImageData(25*r.cw,0,r.cw,image.height).data;
    let inkPixels=0,fullHeightColumns=0;
    for(let x=0;x<r.cw;x++){
     let matched=0;for(let y=0;y<image.height;y++){const i=(y*r.cw+x)*4;
      if(pixels[i]===((r.fg>>16)&255)&&pixels[i+1]===((r.fg>>8)&255)&&pixels[i+2]===(r.fg&255))matched++;
     }
     inkPixels+=matched;if(matched===image.height)fullHeightColumns++;
    }
    const background=[...pixels.slice(0,3)];
    const luminance=rgb=>rgb.map(c=>c/255).map(c=>c<=.04045?c/12.92:((c+.055)/1.055)**2.4).reduce((sum,c,i)=>sum+c*[.2126,.7152,.0722][i],0);
    const fg=[r.fg>>16&255,r.fg>>8&255,r.fg&255],a=luminance(fg),b=luminance(background);
    images.push({state:r.state,fg,background,inkPixels,fullHeightColumns,height:image.height,contrast:(Math.max(a,b)+.05)/(Math.min(a,b)+.05),pixels:[...pixels]});
   }
   let changedPixels=0;for(let i=0;i<images[0].pixels.length;i+=4)if(images[0].pixels.slice(i,i+4).some((v,j)=>v!==images[1].pixels[i+j]))changedPixels++;
   for(const r of images)delete r.pixels;
   return {images,changedPixels,thicknessRatio:images[1].fullHeightColumns/images[0].fullHeightColumns};
  },input);
  assert(measured.thicknessRatio>1,`${theme}: hover must be thicker than the normal line`);
  assert(measured.images[0].fullHeightColumns>0,`${theme}: normal line must remain continuous`);
  results.push({theme,...measured});
 }
}finally{await browser.close();}
fs.writeFileSync(path.join(root,'hover-pixels.json'),JSON.stringify(results,null,2));
console.log(JSON.stringify(results));
