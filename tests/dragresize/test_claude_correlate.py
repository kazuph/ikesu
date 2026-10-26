#!/usr/bin/env python3
import json,subprocess,sys,tempfile,unittest
from pathlib import Path
class ClaudeFrameCorrelationTest(unittest.TestCase):
 def test_requires_exact_current_size_frame_hash(self):
  with tempfile.TemporaryDirectory() as tmp:
   d=Path(tmp)
   def put(name,values):(d/name).write_text(''.join(json.dumps(v)+'\n' for v in values))
   (d/'metadata.json').write_text(json.dumps(dict(trial=dict(kind='vertical'))))
   put('claude-1.jsonl',[dict(type='claude.pty_size',clock_ns=120,size_sequence=1,width=11,height=3),dict(type='claude.frame',clock_ns=125,size_sequence=1,width=11,height=3,text_hash='current',changed_since_resize=False,synchronized=False),dict(type='claude.frame',clock_ns=140,size_sequence=1,width=11,height=3,text_hash='current',changed_since_resize=True,synchronized=False)])
   put('claude-2.jsonl',[dict(type='claude.frame',clock_ns=90,size_sequence=0,width=10,height=3,text_hash='unchanged',changed_since_resize=True,synchronized=False)])
   def bodies(at,hash):return dict(type='bodies',clock_ns=at,synchronized=True,bodies=[dict(X=1,Y=1,Width=11,Height=3,Hash=hash),dict(X=20,Y=1,Width=10,Height=3,Hash='unchanged')])
   put('timeline.jsonl',[dict(type='frame',clock_ns=130,corners=[]),bodies(130,'current'),dict(type='frame',clock_ns=150,corners=[]),bodies(150,'current')])
   subprocess.run([sys.executable,str(Path(__file__).with_name('correlate_claude_wait.py')),str(d)],check=True,stdout=subprocess.DEVNULL)
   frames=[json.loads(line) for line in (d/'timeline-correlated.jsonl').read_text().splitlines()]
   self.assertEqual([v['ID'] for v in frames[0]['panes']],[2])
   self.assertEqual([v['ID'] for v in frames[1]['panes']],[1,2])
   proof=json.loads((d/'claude-correlation.json').read_text());self.assertEqual(proof['exactly_matched_frames'],1)
   first=next(v for v in proof['observations'] if v['id']==1)
   self.assertFalse(first['source_synchronized'])
   self.assertEqual((first['pty_size_ns'],first['app_source_seen_ns'],first['app_visible_ns']),(120,140,150))
if __name__=='__main__':unittest.main()
