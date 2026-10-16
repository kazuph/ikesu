#!/usr/bin/env python3
"""Generate stage6a identical-input procedures for both products."""
import json
from pathlib import Path
import sys
out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
def send(s): return 'send '+json.dumps(s)+'\n'
def snap(s): return f'sleep 300ms\nscreen @OUT@/{s}.txt\ncells @OUT@/{s}.json\n'
def action(s): return send(f'python3 @FIXTURES@/stage6a_action.py {s}\r')+'sleep 700ms\n'
for name in ['duplicate','cross']:
    body=action(name)+snap('open')+send('\x1b')+snap('escape')+send('Z')+snap('typed')+'kill\n'
    (out/f'popup-{name}.steps').write_text(body)
for width in [6,5,3]:
    (out/f'popup-size-{width}.steps').write_text(action(f'size {width} 3')+snap('open')+'kill\n')
(out/'popup-small-screen.steps').write_text('resize 5 3\n'+action('size 6 4')+snap('open')+'kill\n')
(out/'popup-user.steps').write_text(send('\x14t')+snap('open')+send('stty size\r')+snap('size')+send('\x1b')+snap('escape')+'kill\n')
body=send('\x14r')+snap('normal')+send('\x1b')
for w in [56,20]:
    body+=f'resize {w} 7\n'+send('\x14r')+snap(f'width-{w}')+send('\x1b')
(out/'name-narrow.steps').write_text(body+'kill\n')
(out/'menu-short.steps').write_text('resize 80 10\nrclick 60 4\n'+snap('open')+send('\x1b[B'*20)+snap('last')+'kill\n')
for name,label in [('worktree-list','Open worktree...'),('worktree-create','New worktree'),('worktree-group','Close')]:
    body=action('repo')+'rclick 5 5\n'+snap('menu')+f'clicktext "{label}"\n'+snap('dialog')
    if name=='worktree-create': body+=send('main\r')+snap('failed')
    if name=='worktree-list': body+=send('branch-1')+snap('search')+send('\x1b[B'*12)+snap('last')
    (out/f'{name}.steps').write_text(body+'kill\n')
