#!/usr/bin/env python3
"""Generate stage-seven copy inputs shared by the two isolated binaries."""
from pathlib import Path
import json
import sys

out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
setup = "printf '\\033[3J\\033[H\\033[2J'; for i in {1..120}; do printf 'ROW-%03d alpha ALPHA Beta abcdefghijklmnopqrstuvwxyz\\n' $i; done; sleep 120\r"

def send(value):
    return 'send ' + json.dumps(value) + '\nsleep 200ms\n'

def save(name):
    return f'screen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'

cases = {
    'k23-pages': send('\x14[')+save('open')+send('\x02')+save('back')+send('\x06')+save('forward'),
    'k24-pane-return': send('\x14v')+send('\x14h')+send('\x14[')+send('/alpha\r')+send('vkk')+save('selected')+send('\x14l')+save('away')+send('\x14h')+save('returned'),
    'k25-search': send('\x14[')+send('/alpha\r')+save('lower')+send('/Alpha\r')+save('upper')+send('/alpha\r')+save('committed')+send('/Beta')+send('\x1b')+save('cancelled')+send('/\r')+save('empty')+send('/Beta\x15')+save('cleared')+send('\x1b')+send('n')+save('repeated'),
    'k26-lines': send('\x14[')+send('gllllVj')+save('selected')+send('y')+save('copied'),
    'k27-scrolled': 'mouse wheel-up 80 20\n'*8+'sleep 200ms\n'+save('before')+send('\x14[')+save('opened')+send('q')+save('closed'),
    'k28-drag': 'mouse down 80 3\nsleep 800ms\n'+save('click-edge')+'mouse up 80 3\nmouse down 80 20\nmouse motion 80 3\nsleep 800ms\n'+save('drag-edge')+'mouse motion 80 2\nsleep 200ms\n'+save('drag-outside')+'mouse up 80 2\n',
    'k29-resize': send('\x14[')+send('/alpha\r')+save('searched')+'resize 100 25\nsleep 200ms\n'+save('resized')+send('n')+save('repeated'),
    'k28-boundaries': 'mouse down 80 3\nsleep 400ms\n'+save('click-edge')+'mouse up 80 3\nmouse down 80 20\nmouse motion 80 3\nsleep 4s\n'+save('oldest')+'mouse motion 80 43\nsleep 4s\n'+save('newest')+'mouse up 80 43\n'+save('released'),
    'k28-modifiers': send('\x1b[<4;80;20M')+send('\x1b[<36;80;3M')+save('shift-drag')+send('\x1b[<4;80;3m')+send('\x1b[<4;159;20M')+save('shift-scrollbar')+send('\x1b[<4;159;20m'),
    'k29-folded': send('\x14b')+send('\x14[')+send('/alpha\r')+save('searched')+'resize 100 25\nsleep 400ms\n'+save('resized')+send('n')+save('repeated'),
    'k28-down': 'mouse wheel-up 80 20\n'*16+'sleep 200ms\n'+save('before')+'mouse down 80 20\nmouse motion 80 43\nsleep 400ms\n'+save('edge')+'mouse motion 80 45\nsleep 200ms\n'+save('outside')+'mouse up 80 45\n',
    'k29-width': send('\x14[')+send('/alpha\r')+save('searched')+'resize 100 45\nsleep 400ms\n'+save('resized')+send('n')+save('repeated'),
    'k30-fallback': send('\x14[')+send('g0vll')+save('selected')+send('y')+save('copied'),
}
for direction, y in [('up-one', 2), ('up-two', 1), ('down-one', 44), ('down-two', 45)]:
    cases['k28-immediate-'+direction] = ('mouse wheel-up 80 20\n'*8+'sleep 200ms\n'+save('before')+
        f'mouse down 80 20\nmouse motion 80 {y}\nmouse up 80 {y}\nsleep 200ms\n'+save('released'))

timing = 'mouse down 80 20\nmouse motion 80 3\n'
for i in range(6):
    timing += save(f'tick-{i}')+'sleep 100ms\n'
cases['k28-timing'] = timing+'mouse motion 80 1\n'+save('outside')+'mouse up 80 1\n'

for name, body in cases.items():
    # Split before producing history so both panes have the same real shell fixture.
    if name == 'k24-pane-return':
        body = body.replace(send('\x14v')+send('\x14h'), '')
        pre = send('\x14v')+send('\x14h')
    else:
        pre = ''
    fixture = setup.replace('abcdefghijklmnopqrstuvwxyz', 'abcdefghijklmnopqrstuvwxyz'*4) if name.startswith('k29-') else setup
    (out/(name+'.steps')).write_text(pre+send(fixture)+'sleep 400ms\n'+body+'kill\n')
