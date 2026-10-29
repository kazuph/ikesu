import test from 'node:test';
import assert from 'node:assert/strict';
import {buildDialogSpec,locationLabel} from '../spec.mjs';
import {buildDecisionBlocks,fallbackText} from '../../slack-decisions/slack-decisions.mjs';
import {discoverRichContent} from '../rich.mjs';

test('Mac and Slack lead with the same four complete location fields and exact approval command',()=>{
 const d={decision_id:'dec-display',kind:'command_guard',title:'削除操作は確認が必要です',body:'規則: delete\n申請文: 必要な削除',origin:{cwd:'/repo/ikesu',workspace_label:'開発',pane_label:'実装',agent:'codex',command:'bash delete.sh',target_command:'rm /tmp/target'},options:[{id:'approve',label:'承認'},{id:'reject',label:'却下'}]};
 for (const machine of [{id:'local'},{id:'mini',label:'Mac mini'}]) {
  const spec=buildDialogSpec(d,machine),blocks=buildDecisionBlocks({decision:d,machine});
  assert.ok(blocks[0].elements[0].text.startsWith(spec.location));
  assert.equal(spec.location.split('｜').length,4);
  assert.equal(spec.approval_command,'rm /tmp/target');
  assert.match(blocks[1].text.text,/実行するコマンド\*\n```\nrm \/tmp\/target\n```/);
  assert.equal(blocks[2].text.text,'*削除操作は確認が必要です*');
  assert.equal(blocks[3].text.text,d.body);
  assert.ok(fallbackText({decision:d,machine}).startsWith(spec.location));
  assert.deepEqual(spec.options.map(x=>x.id),['approve','reject']);
 }
 assert.equal(locationLabel({}), 'ローカル｜不明｜不明｜不明');
 const long='名前'.repeat(2000);assert.ok(locationLabel({origin:{workspace_label:long}}).includes(long));
});

test('video discovery retains file/URL destinations and encoded paths without redacted data',()=>{
 const sources=['/tmp/日本語 動画.mp4','/tmp/clip.mov','https://example.com/a.webm?download=1','/tmp/clip.m4v'];
 const body=['動画: '+sources[0],'`'+sources[1]+'`',sources[2],'[動画](/tmp/clip.m4v)','/tmp/secret***/private.mp4'].join('\n');
 const found=discoverRichContent({body});
 assert.deepEqual(found.videos,sources);
 const spec=buildDialogSpec({body,options:[]},{id:'local'});
 assert.ok(sources.every(source=>spec.media_sources.includes(source)));
 assert.ok(!JSON.stringify(found).includes('***'));
});
