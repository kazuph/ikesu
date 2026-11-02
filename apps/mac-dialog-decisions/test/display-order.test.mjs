import test from 'node:test';
import assert from 'node:assert/strict';
import {buildDialogSpec,locationLabel,approvalTargetRange,approvalFields} from '../spec.mjs';
import {buildDecisionBlocks,fallbackText} from '../../slack-decisions/slack-decisions.mjs';
import {discoverRichContent} from '../rich.mjs';

test('Mac and Slack lead with the same four complete location fields and exact approval command',()=>{
 const d={decision_id:'dec-display',kind:'command_guard',title:'削除操作は確認が必要です',body:'規則: delete\n申請文: 必要な削除',origin:{cwd:'/repo/ikesu',workspace_label:'開発',pane_label:'実装',agent:'codex',command:'bash delete.sh',target_command:'rm /tmp/target'},options:[{id:'approve',label:'承認'},{id:'reject',label:'却下'}]};
 for (const machine of [{id:'local'},{id:'mini',label:'Mac mini'}]) {
  const spec=buildDialogSpec(d,machine),blocks=buildDecisionBlocks({decision:d,machine});
  assert.ok(blocks[0].elements[0].text.startsWith(spec.location));
  assert.equal(spec.location.split('｜').length,4);
  assert.equal(spec.approval_command,'bash delete.sh');
  assert.equal(spec.approval_target_range,null);
  assert.equal(blocks[1].text.text,'*削除操作は確認が必要です*');
  assert.equal(blocks[2].elements[0].elements[0].text,'bash delete.sh');
  assert.equal(blocks[3].text.text,d.body);
  assert.ok(fallbackText({decision:d,machine}).startsWith(spec.location));
  assert.deepEqual(spec.options.map(x=>x.id),['approve','reject']);
 }
 assert.equal(locationLabel({}), '不明');
 assert.equal(locationLabel({origin:{cwd:'/repo/ikesu',workspace_label:'ikesu',pane_label:'作業'}},{id:'local'}), 'ローカル｜ikesu｜作業');
 const long='名前'.repeat(2000);assert.ok(locationLabel({origin:{workspace_label:long}}).includes(long));
});

test('guard target is highlighted only when its location is unambiguous',()=>{
 const guard=(command,target)=>({kind:'command_guard',origin:{command,target_command:target}});
 assert.deepEqual(approvalTargetRange(guard('rm a && grep x','rm a')),{start:0,length:4});
 assert.deepEqual(approvalTargetRange(guard('rm a','')),{start:0,length:4});
 for(const [command,target] of [['rm a && rm a','rm a'],['echo rm a','rm a'],['rm "$X" && echo x','rm "$X"'],['rm *** && echo x','rm ***']]) assert.equal(approvalTargetRange(guard(command,target)),null);
});

test('request fields retain commands quoted by the requester',()=>{
 const command = "printf '対象コマンド: rm /tmp/a'";
 const body = `対象プロジェクト: ikesu。必要な理由: 表示確認。実施内容: ${command}。確認済み: 内容を確認。戻し方: 中止。`;
 const fields = approvalFields(body);
 assert.deepEqual(fields.map(f=>f.label),['対象','理由','内容','確認済み','戻し方']);
 assert.ok(fields.find(f=>f.label==='内容').value.includes(command));
 assert.equal(approvalFields('対象プロジェクト: x。必要な理由: x。必要な理由: y'),null);
 const commandWithLabel = "printf '戻し方: 元のまま'";
 const spaced = `対象プロジェクト: ikesu 必要な理由: 検証 実施内容: ${commandWithLabel} 確認済み: 内容 戻し方: 中止`;
 const spacedFields = approvalFields(spaced);
 assert.deepEqual(spacedFields.map(f=>f.label),['対象','理由','内容','確認済み','戻し方']);
 assert.ok(spacedFields.find(f=>f.label==='内容').value.includes(commandWithLabel));
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
