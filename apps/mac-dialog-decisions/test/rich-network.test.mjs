import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {fetchPublic,publicAddress,pageMetadata} from '../rich.mjs';

test('real public requests record only public peers, carry no cookies/auth and enforce redirect/size/deadline',async()=>{
 const peers=[];const opts={onConnect:peer=>peers.push(peer)};
 const page=await fetchPublic('https://example.com',opts);
 assert.match((await pageMetadata(page.data,page.url)).title,/Example Domain/);
 const headers=JSON.parse((await fetchPublic('https://httpbin.org/headers',opts)).data).headers;
 assert.equal(headers.Cookie,undefined);assert.equal(headers.Authorization,undefined);
 await fetchPublic('https://httpbin.org/redirect/3',opts);
 await assert.rejects(fetchPublic('https://httpbin.org/redirect/4',opts),/転送回数/);
 await assert.rejects(fetchPublic('https://httpbin.org/bytes/1024',{...opts,maxBytes:10}),/サイズ/);
 await assert.rejects(fetchPublic('https://httpbin.org/delay/10',opts),/abort|timeout/i);
 assert.ok(peers.length>0);assert.ok(peers.every(peer=>publicAddress(peer.address)));
 console.log('PUBLIC_CONNECTIONS',JSON.stringify(peers));
});

test('a real public redirect and og:image cannot connect to a private destination',async()=>{
 let connections=0;const server=http.createServer((req,res)=>{connections++;res.end('private');});
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 const local=`http://127.0.0.1:${server.address().port}/private.png`;
 try{
  await assert.rejects(fetchPublic(`https://httpbin.org/redirect-to?url=${encodeURIComponent(local)}`),/非公開/);
  const html=`<html><title>safe</title><meta property="og:image" content="${local}"></html>`;
  const page=await fetchPublic(`https://httpbin.org/base64/${encodeURIComponent(Buffer.from(html).toString('base64'))}?content-type=text/html`);
  const metadata=await pageMetadata(page.data,page.url);
  assert.equal(metadata.image,local);await assert.rejects(fetchPublic(metadata.image),/非公開/);
  assert.equal(connections,0);console.log('PRIVATE_CONNECTIONS',connections);
 }finally{await new Promise(resolve=>server.close(resolve));}
});
