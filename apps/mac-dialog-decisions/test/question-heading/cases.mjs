export function headingCases() {
 const body='この変更を取り込んでよいですか？\n期待する動作と確認した結果を残します。';
 const base={kind:'agent_prompt',decision_id:'heading-test',status:'pending',options:[{id:'yes',label:'進める'},{id:'no',label:'見直す'}],allow_text:true,multi_select:false,origin:{cwd:'/isolated/ikesu',workspace_label:'質問表示試験',pane_label:'隔離パネル',agent:'claude'}};
 return [
  {name:'short',decision:{...base,title:'取り込み確認',body},heading:'取り込み確認',body},
  {name:'absent',decision:{...base,title:'',body},heading:'',body},
  {name:'same',decision:{...base,title:body,body},heading:'',body},
  {name:'prefix',decision:{...base,title:'この変更を取り込んでよいですか？',body},heading:'',body},
  {name:'claude-header',decision:{...base,title:body,body:'質問 1/2 取り込み',question_label:'質問 1/2 取り込み'},heading:'質問 1/2 取り込み',body},
  {name:'label-same',decision:{...base,title:body,body,question_label:body},heading:'',body},
  {name:'label-prefix',decision:{...base,title:body,body,question_label:'この変更を取り込んでよいですか？'},heading:'',body},
  {name:'title-only',decision:{...base,title:body,body:null},heading:'',body},
  {name:'guard',decision:{...base,kind:'command_guard',title:'実行確認',body:'対象コマンド：rm /tmp/isolated-example',origin:{...base.origin,target_command:'rm /tmp/isolated-example'}},heading:'実行確認',body:'対象コマンド：rm /tmp/isolated-example'},
 ];
}
