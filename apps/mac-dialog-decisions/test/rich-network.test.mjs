import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {once} from 'node:events';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {fetchPublic,publicAddress,pageMetadata,RICH_LIMITS,RICH_USER_AGENT} from '../rich.mjs';
import {RichService} from '../rich-service.mjs';

// The public service is emulated on loopback. Only DNS and transport are
// injected; every redirect still passes the production address validator.
async function publicService(t) {
 const requests=[],resolved=[];
 const server=http.createServer((req,res)=>{
  requests.push(req.url);
  const url=new URL(req.url,'http://public.test');
  const redirect=url.pathname.match(/^\/redirect\/(\d+)$/);
  if(redirect&&Number(redirect[1])>0){res.writeHead(302,{Location:`/redirect/${Number(redirect[1])-1}`});res.end();}
  else if(url.pathname==='/redirect-to'){res.writeHead(302,{Location:url.searchParams.get('url')});res.end();}
  // Like Wikimedia: refuse a request without a User-Agent with HTTP 403.
  else if(url.pathname.startsWith('/wiki/')&&!req.headers['user-agent']){res.writeHead(403,{'Content-Type':'text/plain'});res.end('Please set a user-agent');}
  else if(url.pathname==='/wiki/File:Gyuu-don_001.jpg'){res.writeHead(200,{'Content-Type':'text/html; charset=UTF-8'});res.end('<html><title>File:Gyuu-don 001.jpg</title><meta property="og:site_name" content="Wikimedia Commons"></html>');}
  else if(url.pathname==='/wiki/photo.png'){res.writeHead(200,{'Content-Type':'image/png'});res.end(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==','base64'));}
  else if(url.pathname==='/headers'){res.end(JSON.stringify({headers:req.headers}));}
  else if(url.pathname==='/bytes'){res.end(Buffer.alloc(1024));}
  else if(url.pathname==='/delay'){server.emit('delayed');}
  else if(url.pathname==='/page'){res.end(`<html><title>safe</title><meta property="og:image" content="${url.searchParams.get('image')}"></html>`);}
  else {res.end('<html><title>Public fixture</title></html>');}
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 t.after(async()=>{server.closeAllConnections();await new Promise(resolve=>server.close(resolve));});
 const network={
  lookup:async(host,options)=>{
   assert.deepEqual(options,{all:true});resolved.push(host);
   if(host==='public.test')return [{address:'8.8.8.8',family:4}];
   if(host==='internal.test')return [{address:'127.0.0.1',family:4}];
   if(host==='mixed.test')return [{address:'8.8.8.8',family:4},{address:'10.0.0.1',family:4}];
   assert.fail(`unexpected DNS request: ${host}`);
  },
  request:(url,options,onResponse)=>{
   assert.equal(url.hostname,'public.test');
   // Verify the address pinned by production code before routing the actual
   // HTTP socket to the local public-service emulator.
   options.lookup(url.hostname,{},(error,address,family)=>{
    assert.equal(error,null);assert.equal(address,'8.8.8.8');assert.equal(family,4);assert.ok(publicAddress(address));
   });
   return http.get(new URL(url.pathname+url.search,`http://127.0.0.1:${server.address().port}`),options,onResponse);
  }
 };
 return {network,requests,resolved,server};
}

test('public-service emulator carries no cookies/auth and enforces redirect/size/deadline over real HTTP',async t=>{
 const {network,requests}=await publicService(t);
 const peers=[];const opts={...network,onConnect:peer=>peers.push(peer)};
 const page=await fetchPublic('http://public.test/',opts);
 assert.equal((await pageMetadata(page.data,page.url)).title,'Public fixture');
 const headers=JSON.parse((await fetchPublic('http://public.test/headers',opts)).data).headers;
 assert.equal(headers.cookie,undefined);assert.equal(headers.authorization,undefined);
 await fetchPublic('http://public.test/redirect/3',opts);
 await assert.rejects(fetchPublic('http://public.test/redirect/4',opts),/転送回数/);
 await assert.rejects(fetchPublic('http://public.test/bytes',{...opts,maxBytes:10}),/サイズ/);
 // This test takes five seconds to verify the existing production deadline
 // against a real connected server that never sends a response.
 const start=Date.now();
 await assert.rejects(fetchPublic('http://public.test/delay',opts),/abort|timeout/i);
 assert.ok(Date.now()-start>=RICH_LIMITS.timeoutMs);
 assert.ok(requests.includes('/delay'));assert.ok(peers.length>0);
 assert.ok(peers.every(peer=>peer.address==='127.0.0.1'&&new URL(peer.url).hostname==='public.test'));
});

test('public redirects and og:image reject literal, DNS-resolved and mixed private destinations with zero private connections',async t=>{
 const {network,requests,resolved}=await publicService(t);
 let connections=0;const server=http.createServer((req,res)=>{connections++;res.end('private');});
 let sockets=0;server.on('connection',()=>sockets++);
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const local=`http://127.0.0.1:${server.address().port}/private.png`;
 try{
  for(const target of [local,`http://internal.test:${server.address().port}/private.png`,`http://mixed.test:${server.address().port}/private.png`]){
   const before=requests.length;
   await assert.rejects(fetchPublic(`http://public.test/redirect-to?url=${encodeURIComponent(target)}`,network),/非公開/);
   assert.equal(requests.length,before+1);
  }
  const page=await fetchPublic(`http://public.test/page?image=${encodeURIComponent(local)}`,network);
  const metadata=await pageMetadata(page.data,page.url);
  assert.equal(metadata.image,local);await assert.rejects(fetchPublic(metadata.image,network),/非公開/);
  assert.ok(resolved.includes('internal.test'));assert.ok(resolved.includes('mixed.test'));
  assert.equal(connections,0);assert.equal(sockets,0);console.log('PRIVATE_CONNECTIONS',connections);
 }finally{await new Promise(resolve=>server.close(resolve));}
});

test('closing a window cancels its connected public-service request with the caller reason',async t=>{
 const {network,server}=await publicService(t);
 const controller=new AbortController();
 const connected=once(server,'delayed');
 const pending=fetchPublic('http://public.test/delay',{...network,signal:controller.signal});
 await connected;
 const reason=Error('window closed');controller.abort(reason);
 await assert.rejects(pending,error=>{
  assert.equal(error.code,'ABORT_ERR');assert.equal(error.cause,reason);return true;
 });
});

test('an image URL that is really a page (Wikimedia File:) shows as a link card, and requests carry a User-Agent',async t=>{
 const {network}=await publicService(t);
 const headers=JSON.parse((await fetchPublic('http://public.test/headers',network)).data).headers;
 assert.equal(headers['user-agent'],RICH_USER_AGENT);
 const root=await fs.mkdtemp(path.join(os.tmpdir(),'dialog-rich-card-'));t.after(()=>fs.rm(root,{recursive:true,force:true}));
 const service=new RichService({stateDir:root,network});
 const published=[];const lifetime=new AbortController();t.after(()=>lifetime.abort());
 await service.load({body:'・出典 http://public.test/wiki/File:Gyuu-don_001.jpg\n画像 http://public.test/wiki/photo.png'},'local',lifetime.signal,message=>published.push(message));
 assert.deepEqual(published.filter(m=>m.status==='rich_error'),[]);
 const card=published.find(m=>m.status==='rich_card');
 // Android stores cards by index: the only card here comes first.
 assert.equal(card?.index,0);
 assert.equal(card?.url,'http://public.test/wiki/File:Gyuu-don_001.jpg');
 assert.equal(card.title,'File:Gyuu-don 001.jpg');assert.equal(card.site,'Wikimedia Commons');
 const image=published.find(m=>m.status==='rich_image');
 assert.equal(image?.source,'http://public.test/wiki/photo.png');
 assert.equal((await fs.readFile(image.path)).subarray(1,4).toString(),'PNG');
});
