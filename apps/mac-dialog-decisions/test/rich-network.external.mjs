// Optional Internet integration: exercises real DNS, TLS and public peers.
// Private redirect enforcement is covered by the local HTTP emulator.
import test from 'node:test';
import assert from 'node:assert/strict';
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

test('closing a window cancels its actual public request with the caller reason',async()=>{
 const controller=new AbortController();
 const pending=fetchPublic('https://httpbin.org/delay/10',{signal:controller.signal});
 controller.abort(Error('window closed'));
 await assert.rejects(pending,/window closed/);
});
