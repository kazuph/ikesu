import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { discoverRichContent, publicAddress, fetchPublic, pageMetadata } from '../rich.mjs';

test('rich discovery uses visible fields only, deduplicates and applies agreed counts', () => {
  const found = discoverRichContent({title:'https://example.com/a', body:'画像: /tmp/日本語 添付/1.png\nhttps://example.com/a\nhttps://example.com/b.jpg\nhttps://example.com/c\nhttps://example.com/d', options:[{description:'https://example.com/e'}], details:[{value:'https://secret.example/'}]});
  assert.deepEqual(found.links, ['https://example.com/a','https://example.com/b.jpg','https://example.com/c']);
  assert.deepEqual(found.images, ['/tmp/日本語 添付/1.png','https://example.com/b.jpg']);
  assert.equal(JSON.stringify(found).includes('secret.example'), false);
  assert.deepEqual(discoverRichContent({body:'https://example.com/?token=***'}), {links:[], images:[]});
});

test('automatic requests reject nonpublic addresses including IPv4-mapped IPv6', () => {
  for (const address of ['127.0.0.1','10.0.0.1','172.16.0.1','192.168.1.1','100.64.0.1','100.127.255.255','169.254.1.1','0.0.0.0','224.0.0.1','::1','::','fc00::1','fe80::1','::ffff:127.0.0.1','::ffff:7f00:1','2001:db8::1']) assert.equal(publicAddress(address), false, address);
  for (const address of ['1.1.1.1','8.8.8.8','2606:4700:4700::1111']) assert.equal(publicAddress(address), true, address);
});

test('forbidden URLs make zero actual connections to a local server', async () => {
  let connections=0;
  const server=http.createServer((req,res)=>{ connections++; res.end('private'); });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const port=server.address().port;
  try {
    for(const host of ['127.0.0.1','localhost','[::1]']) await assert.rejects(fetchPublic(`http://${host}:${port}/`));
    for(const url of ['https://host.ts.net/a','https://u:p@example.com/a','https://example.com/***','https://example.com/?token=secret']) await assert.rejects(fetchPublic(url));
    assert.equal(connections,0);
  } finally { await new Promise(resolve=>server.close(resolve)); }
});

test('metadata is parsed as text, resolves og image and never evaluates markup', async () => {
  const metadata=await pageMetadata(Buffer.from('<html><head><title>A &amp; B</title><meta property="og:site_name" content="日本語"><meta property="og:image" content="/image.png"></head><body><script>process.exit(9)</script></body></html>'), 'https://example.com/path');
  assert.deepEqual(metadata,{title:'A & B',site:'日本語',image:'https://example.com/image.png'});
});

test('image discovery preserves prose and Markdown boundaries without adding punctuation or redacted candidates',()=>{
 const body=['結果は /tmp/after.png です','スクショ: `/tmp/code.png`','[画像](/tmp/日本語.png)','![画像](/tmp/日本語%20空白.png)','"/tmp/日本語 空白.png"','`https://example.com/image.png`','[画像](https://example.com/linked.jpg)','画像: /tmp/秘密***/secret.png'].join('\n');
 const found=discoverRichContent({body});
 assert.deepEqual(found.images,['/tmp/after.png','/tmp/code.png','/tmp/日本語.png','/tmp/日本語 空白.png']);
 assert.deepEqual(discoverRichContent({body:'"/tmp/日本語 空白.png"\n`https://example.com/image.png`\n[画像](https://example.com/linked.jpg)'}).images,['/tmp/日本語 空白.png','https://example.com/image.png','https://example.com/linked.jpg']);
 assert.equal(JSON.stringify(found).includes('***'),false);
});
