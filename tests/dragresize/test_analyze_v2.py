#!/usr/bin/env python3
import json,subprocess,sys,tempfile,unittest
from pathlib import Path
class CorrelationTest(unittest.TestCase):
 def test_border_does_not_prove_application_caught_up(self):
  with tempfile.TemporaryDirectory() as name:
   d=Path(name)
   def frame(at,x,width,layout):return dict(type='frame',clock_ns=at,bytes=1,corners=[dict(X=x,Y=2,Glyph='┐')],panes=[dict(ID=1,Seq=2 if width==11 else 1,Width=width,Height=3,LayoutWidth=layout,LayoutHeight=3)])
   records=[dict(type='trial',clock_ns=50,config=dict(kind='vertical',count=1,rate=4)),frame(90,10,10,10),dict(type='input',clock_ns=100,index=0,watch_x=11,watch_y=2),frame(130,11,10,11),frame(170,11,11,11),dict(type='release',clock_ns=180,watch_x=11,watch_y=2),dict(type='finish',clock_ns=200)]
   (d/'timeline.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in records));(d/'metadata.json').write_text(json.dumps(dict(scenario=dict(panes=1))))
   app=[dict(event='pty_size',clock_ns=150,id=1,seq=2,width=11,height=3),dict(event='app_written',clock_ns=160,id=1,seq=2,width=11,height=3)]
   (d/'app-1.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in app))
   subprocess.run([sys.executable,str(Path(__file__).with_name('analyze_v2.py')),str(d)],check=True,stdout=subprocess.DEVNULL)
   result=json.loads((d/'analysis.json').read_text())
   self.assertEqual(result['slow']['median'],30/1e6)
   self.assertEqual(result['application']['median'],70/1e6)
   self.assertEqual(result['pty_delivery']['median'],50/1e6)
   self.assertEqual(result['release_settle_ms'],0)
   self.assertTrue(result['final_dimensions_verified'])
if __name__=='__main__':unittest.main()
