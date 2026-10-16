#!/usr/bin/env python3
import json
from pathlib import Path
import sys
root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=True)
def send(s):return 'send '+json.dumps(s,ensure_ascii=False)+'\nsleep 300ms\n'
def shot(n):return f'screen @OUT@/{n}.txt\ncells @OUT@/{n}.json\n'
for kind,label in [('ascii','ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ'),('wide','日本語の長いペインタイトルを表示して省略記号を確認する')]:
 for mode in ['saved','unsaved']:
  steps=send('\x14P')+send('\x03'+label+'\r')+send(f'python3 @FIXTURES@/stage5_agent.py {mode}\r')
  for width in [34,40,50]:steps+=f'resize {26+width} 30\nsleep 300ms\n'+shot(f'width-{width}')
  (root/f'title-{kind}-{mode}.steps').write_text(steps+'kill\n')
(root/'failure.steps').write_text(send('touch @OUT@/close-ready\r')+'waitfile @OUT@/close-finished 10s\nsleep 300ms\n'+shot('failed')+'kill\n')
