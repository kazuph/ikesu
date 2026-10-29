import http from 'node:http';
import {pipeline} from 'node:stream/promises';
import https from 'node:https';
import dns from 'node:dns/promises';
import net from 'node:net';
import { execFile } from 'node:child_process';

// Limits from the approved dialog-rich plan, revision 1.
export const RICH_LIMITS = Object.freeze({cards:3, images:4, imageBytes:10*1024*1024, pageBytes:2*1024*1024, redirects:3, timeoutMs:5000, pixels:8000, attachmentBytes:20*1024*1024, retentionMs:7*24*60*60*1000});
const videoExtension=/\.(mp4|mov|m4v|webm|ogv|ogg)$/i;
export function videoPath(value) { try { return videoExtension.test(new URL(value).pathname); } catch { return videoExtension.test(value); } }
const imageExtension=/\.(png|jpe?g|gif|webp)$/i;
export function imagePath(value) { try { return imageExtension.test(new URL(value).pathname); } catch { return imageExtension.test(value); } }
function visibleURLs(text) {
  return (text.match(/https?:\/\/[^\s<>"'`「」『』]+/gu)||[]).map(s=>s.replace(/[.,;!?)）\]、。]+$/gu,'')).filter(s=>!s.includes('***'));
}
export function discoverRichContent(spec) {
  const text=[spec.title||'',spec.body||'',...(spec.options||[]).map(o=>o.description||'')].join('\n');
  const links=[...new Set(visibleURLs(text))].slice(0,RICH_LIMITS.cards);
  const images=[]; const videos=[];
  const mediaPath = value => imagePath(value) || videoPath(value);
  for(const line of (spec.body||'').split('\n')) {
    const candidates=[];
    // Code and ordinary file paths are literal; only Markdown destinations
    // carry URL-encoded local paths. Mask spans to prevent a second raw match.
    let plain=line.replace(/`([^`\n]+)`/g,(match,value,index)=>{
      if(mediaPath(value))candidates.push({value,index,encoded:false});
      return ' '.repeat(match.length);
    });
    plain=plain.replace(/!?\[[^\]\n]*\]\(\s*(?:<([^>\n]+)>|([^\s)]+))(?:\s+["'][^"'\n]*["'])?\s*\)/g,(match,angle,bare,index)=>{
      candidates.push({value:angle||bare,index,encoded:true});return ' '.repeat(match.length);
    });
    const whole=plain.match(/^(?:画像|スクショ|添付画像|動画)[:：]\s*(\/.*\.(?:png|jpe?g|gif|webp|mp4|mov|m4v|webm|ogv|ogg))\s*$/i)||plain.match(/^\s*(\/.*\.(?:png|jpe?g|gif|webp|mp4|mov|m4v|webm|ogv|ogg))\s*$/i);
    if(whole)candidates.push({value:whole[1],index:0,encoded:false});
    else {
      for(const match of plain.matchAll(/["'「<](\/[^"'」>\n]+\.(?:png|jpe?g|gif|webp|mp4|mov|m4v|webm|ogv|ogg))["'」>]/gi))candidates.push({value:match[1],index:match.index,encoded:false});
      for(const match of plain.matchAll(/(?:^|[\s(])((?:\/)[^\s<>"'`「」()[\]]+\.(?:png|jpe?g|gif|webp|mp4|mov|m4v|webm|ogv|ogg))(?=$|[\s)\]、。])/gi))candidates.push({value:match[1],index:match.index,encoded:false});
      for(const value of visibleURLs(plain).filter(mediaPath))candidates.push({value,index:plain.indexOf(value),encoded:false});
    }
    for(const candidate of candidates.sort((a,b)=>a.index-b.index)) {
      let source=candidate.value;
      if(candidate.encoded&&source.startsWith('/')) {try {source=decodeURIComponent(source);}catch {continue;}}
      if(mediaPath(source)&&!source.includes('***')&&(source.startsWith('/')||/^https?:/.test(source))) { const list=videoPath(source)?videos:images;if(!list.includes(source))list.push(source); }
    }
  }
  return {links,images:images.slice(0,RICH_LIMITS.images),...(videos.length?{videos:videos.slice(0,RICH_LIMITS.images)}:{})};
}
export function publicAddress(address) {
  const version=net.isIP(address);
  if(version===4) {
    const [a,b]=address.split('.').map(Number);
    return !(a===0||a===10||a===127||a>=224||(a===100&&b>=64&&b<=127)||(a===169&&b===254)||(a===172&&b>=16&&b<=31)||(a===192&&(b===168||b===0))||(a===198&&(b===18||b===19))||(a===192&&b===2)||(a===198&&b===51)||(a===203&&b===0));
  }
  if(version!==6) return false;
  // Only global unicast 2000::/3; exclude transition/documentation ranges.
  const groups=address.split(':');
  const first=parseInt(groups[0],16);
  const second=parseInt(groups[1]||'0',16);
  if(!Number.isFinite(first)||first<0x2000||first>0x3fff) return false;
  return !(first===0x2001&&(second<0x200||second===0xdb8)) && first!==0x2002 && first!==0x3fff;
}
function candidate(raw) {
  const url=new URL(raw);
  const host=url.hostname.replace(/^\[|\]$/g,'').toLowerCase();
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||decodeURIComponent(raw).includes('***')||host==='localhost'||host.endsWith('.localhost')||host.endsWith('.ts.net')||host==='ts.net') throw Error('自動取得しない URL です');
  for(const name of url.searchParams.keys()) if(/^(token|access_token|api_?key|password|secret|authorization)$/i.test(name)) throw Error('資格情報付き URL は取得しません');
  return {url,host};
}
async function abortable(promise,signal) {
 let aborted;
 const stop=new Promise((resolve,reject)=>{aborted=()=>reject(signal.reason);signal.addEventListener('abort',aborted,{once:true});if(signal.aborted)aborted();});
 try {return await Promise.race([promise,stop]);} finally {signal.removeEventListener('abort',aborted);}
}
function publicRequest(url,options,onResponse) {
  return (url.protocol==='https:'?https:http).get(url,options,onResponse);
}
export async function fetchPublic(raw,{signal,maxBytes=RICH_LIMITS.pageBytes,onConnect,destination,lookup=dns.lookup,request=publicRequest}={}) {
  const timeout=AbortSignal.timeout(RICH_LIMITS.timeoutMs);
  const controller=new AbortController();
  const sources=[signal,timeout].filter(Boolean);
  const cancel=event=>controller.abort(event.target.reason);
  for(const source of sources) {
    source.addEventListener('abort',cancel,{once:true});
    if(source.aborted) controller.abort(source.reason);
  }
  const combined=controller.signal;
  try {
  let current=raw;
  for(let redirects=0;;redirects++) {
    combined.throwIfAborted();
    const {url,host}=candidate(current);
    const addresses=net.isIP(host)?[{address:host,family:net.isIP(host)}]:await abortable(lookup(host,{all:true}),combined);
    combined.throwIfAborted();
    if(!addresses.length||addresses.some(a=>!publicAddress(a.address))) throw Error('非公開の宛先は自動取得しません');
    const selected=addresses[0];
    const response=await new Promise((resolve,reject)=>{
      const pending=request(url,{agent:false,signal:combined,headers:{Accept:'text/html,image/*', 'Accept-Encoding':'identity'},lookup:(_host,options,callback)=>{if(options.all) callback(null,[selected]); else callback(null,selected.address,selected.family);}},resolve);
      pending.on('socket',socket=>socket.once('connect',()=>onConnect?.({address:socket.remoteAddress,port:socket.remotePort,url:url.href})));
      pending.on('error',reject);
    });
    if([301,302,303,307,308].includes(response.statusCode)) {
      response.destroy();
      if(redirects>=RICH_LIMITS.redirects||!response.headers.location) throw Error('転送回数の上限です');
      current=new URL(response.headers.location,url).href;
      continue;
    }
    if(response.statusCode<200||response.statusCode>=300) { response.destroy(); throw Error(`HTTP ${response.statusCode}`); }
    if(Number(response.headers['content-length'])>maxBytes) {response.destroy();throw Error('取得サイズの上限です');}
    if(destination) { await pipeline(response,destination,{signal:combined});return {url:url.href,type:response.headers['content-type']||''}; }
    const chunks=[]; let bytes=0;
    for await(const chunk of response) { bytes+=chunk.length; if(bytes>maxBytes) {response.destroy();throw Error('取得サイズの上限です');} chunks.push(chunk); }
    return {data:Buffer.concat(chunks),url:url.href,type:response.headers['content-type']||''};
  }
  } finally { for(const source of sources) source.removeEventListener('abort',cancel); }
}
export async function pageMetadata(data,base,signal) {
  // libxml's HTML parser treats metadata as text. --nonet prevents external I/O;
  // entity substitution and DTD loading are deliberately not enabled.
  const parse=(expression)=>new Promise((resolve,reject)=>{
    const child=execFile('/usr/bin/xmllint',['--html','--nonet','--nowarning','--xpath',expression,'-'],{maxBuffer:RICH_LIMITS.pageBytes,timeout:RICH_LIMITS.timeoutMs,signal},(error,stdout)=>{if(error&&!stdout) reject(error); else resolve(stdout.trim());});
    child.stdin.on('error',()=>{}); child.stdin.end(Buffer.concat([Buffer.from('<?xml encoding="UTF-8"?>'),data]));
  });
  const [title,site,image]=await Promise.all([
    parse('string((//meta[@property="og:title"]/@content | //title/text())[1])'),
    parse('string((//meta[@property="og:site_name"]/@content)[1])'),
    parse('string((//meta[@property="og:image"]/@content)[1])')
  ]);
  return {title,site:site||new URL(base).hostname,image:image?new URL(image,base).href:''};
}
