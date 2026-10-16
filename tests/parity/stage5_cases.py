#!/usr/bin/env python3
"""Reproducible stage-five inputs; derived from public UI operations only."""
import json
from pathlib import Path
import sys
out = Path(sys.argv[1]); out.mkdir(parents=True, exist_ok=True)
config = Path(sys.argv[2]).read_text()
def send(text):
    return 'send '+json.dumps(text, ensure_ascii=False)+'\nsleep 200ms\n'
def capture(name):
    return f'screen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'
def case(name, steps, ui=''):
    (out/(name+'.steps')).write_text(steps+'kill\n')
    (out/(name+'.toml')).write_text(config.replace('[ui]\n', '[ui]\n'+ui))
tabs = 'resize 90 30\nsleep 300ms\n'+send('\x14T')+send('\x03TAB01\r')
for i in range(2,16):
    tabs += send('\x14c')+send(f'TAB{i:02}\r')
tabs += capture('created')
for i in range(1,16):
    tabs += send('\x14n')+capture(f'select-{i:02}')
case('tabs-on', tabs)
case('tabs-off', tabs, 'mouse_capture = false\n')
# Same-sized ASCII / wide labels at the same pane width.
for kind, label in [('ascii','ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'),('wide','日本語の長いペインタイトルを表示して省略記号を確認する')]:
    steps='resize 90 30\nsleep 300ms\n'+send('\x14v')+send('\x14P')+send('\x03'+label+'\r')+capture('title')
    case('title-'+kind,steps)
history = send("python3 -c \"print('\\n'.join('LINE%03d'%i for i in range(100)))\"\r")
scroll='resize 100 30\nsleep 300ms\n'+history+send('\x14v')+history+capture('right-focus')+send('\x14h')+capture('left-focus')
case('scroll-bordered',scroll)
case('scroll-borderless',scroll,'pane_borders = false\n')
# Close the real fixture workspace through the isolated server only.
case('empty',send('python3 @FIXTURES@/stage5_empty.py\r')+capture('empty'))
case('narrow',''.join(f'resize {24+w} 30\nsleep 300ms\n'+capture(f'width-{w}')+f'click 25 30\nsleep 200ms\n'+capture(f'click-{w}')+send('\x1b') for w in range(1,6)), 'mobile_width_threshold = 0\n')
