import test from 'node:test';
import assert from 'node:assert/strict';
import * as support from './live_test_support.mjs';
const message='今回の撮影依頼を保存してください。';
const history='過去の会話：'+message+'\n';
const screen=input=>history+'────────────────\n❯ '+input+'\n────────────────\n Opus 5.5\n';
test('履歴に今回の文があっても空入力へEnterを送らない',()=>{
 assert.equal(typeof support.hasUnsentReport,'function');assert.equal(support.hasUnsentReport(screen(''),message),false);
});
test('履歴に今回の文があっても親の別入力へEnterを送らない',()=>{
 assert.equal(support.hasUnsentReport(screen('失敗表示は人が読める文に直して'),message),false);
});
test('今回の文が未送信入力欄にある場合だけEnterを送る',()=>{
 assert.equal(support.hasUnsentReport(screen(message),message),true);
 assert.equal(support.hasUnsentReport(screen(message.slice(0,8)+'\n  '+message.slice(8)),message),true);
 assert.equal(support.hasUnsentReport(history,message),false);
});
