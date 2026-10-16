#!/usr/bin/env python3
"""Generate stage6a identical-input procedures for both products."""
import json
from pathlib import Path
import sys
out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
def send(s): return 'send '+json.dumps(s)+'\n'
def snap(s): return f'sleep 300ms\nscreen @OUT@/{s}.txt\ncells @OUT@/{s}.json\n'
def action(s): return send(f'python3 @FIXTURES@/stage6a_action.py {s}\r')+f'waitfile @HOME@/action-{s.split()[0]}-done.json 15s\nsleep 300ms\n'
for name in ['duplicate','cross']:
    body=action(name)+snap('open')+send('\x1b')+snap('escape')+send('Z')+snap('typed')+'kill\n'
    (out/f'popup-{name}.steps').write_text(body)
for width in [60,8,7,6,5,3]:
    (out/f'popup-size-{width}.steps').write_text(action(f'size {width} 3')+snap('open')+'kill\n')
(out/'popup-small-screen.steps').write_text('resize 5 3\n'+action('size 6 4')+snap('open')+'kill\n')
(out/'popup-user.steps').write_text(send('\x14t')+snap('open')+send('stty size\r')+snap('size')+send('\x1b')+snap('escape')+'kill\n')
body=send('\x14W')+snap('normal')+'click 68 24\n'+snap('normal-click')
for w in [56,20]:
    body+=f'resize {w} 7\n'+send('\x14W')+snap(f'width-{w}')+'click 5 5\n'+snap(f'click-{w}')+send('\x1b')+'sleep 200ms\n'
(out/'name-narrow.steps').write_text(body+'kill\n')
(out/'menu-short.steps').write_text('resize 80 10\nrclick 60 4\n'+snap('open')+send('\x1b[B'*20)+snap('last')+'kill\n')
for name,label in [('worktree-list','Open worktree...'),('worktree-create','New worktree'),('worktree-group','Close')]:
    body=action('repo-group' if name=='worktree-group' else 'repo')+'click 5 7\nsleep 200ms\nrclick 5 7\n'+snap('menu')+f'clicktext "{label}"\n'+snap('dialog')
    if name=='worktree-create': body+=send('main\r')+snap('failed')
    if name=='worktree-list': body+=send('\x1b[B'*12)+snap('tail')+send('/branch-1')+snap('search')+send('\x1b[B'*12)+snap('last')
    if name=='worktree-group': body+=send('\r')+snap('closed')+action('list')
    (out/f'{name}.steps').write_text(body+'kill\n')

(out/'toast-japanese.steps').write_text(action('toast-prepare')+'sleep 1s\n'+action('toast-working')+'sleep 1s\n'+action('toast-finish')+'sleep 2s\n'+snap('toast')+'kill\n')
(out/'worktree-delete.steps').write_text(action('repo-delete')+'click 5 9\nsleep 200ms\nrclick 5 9\n'+snap('menu')+'clicktext "Delete worktree checkout..."\n'+snap('dialog')+'kill\n')

for kind in ['empty','body']:
    (out/f'toast-explicit-{kind}.steps').write_text(action(f'notification-{kind}')+snap('toast')+'kill\n')

body=''
for w in [20,30,40,56]:
    for label,x,y in [('left',5,5),('center',w//2,4),('top',5,3),('outside',1,1)]:
        body+=f'resize {w} 7\n'+send('\x14W')+snap(f'{w}-{label}-before')+f'click {x} {y}\n'+snap(f'{w}-{label}-after')+send('\x1b')+'sleep 200ms\n'
(out/'name-click-probe.steps').write_text(body+'kill\n')

(out/'toast-probe.steps').write_text(action('notification-probe')+snap('toast')+'kill\n')

for position in ['top-left','top-right','bottom-left','bottom-right']:
    (out/f'toast-position-{position}.steps').write_text(action(f'notification-position {position}')+snap('toast')+'kill\n')
for kind in ['trim','blank']:
    (out/f'toast-{kind}.steps').write_text(action(f'notification-{kind}')+snap('toast')+'kill\n')

(out/'toast-lifecycle.steps').write_text(action('notification-empty')+snap('initial')+'click 153 2\n'+snap('dismissed')+action('notification-second')+snap('retry')+'sleep 8s\n'+snap('after-eight')+'sleep 2s\n'+snap('after-ten')+action('notification-third')+snap('third')+'kill\n')

for kind in ['target','expiry']:
    body=action('toast-prepare')+'sleep 1s\n'+action('toast-working')+'sleep 1s\n'+action('toast-finish')+'sleep 2s\n'+snap('visible')
    if kind=='target': body+=send('\x14o')+snap('target')
    else: body+='sleep 8s\n'+snap('expired')
    (out/f'toast-background-{kind}.steps').write_text(body+'kill\n')
