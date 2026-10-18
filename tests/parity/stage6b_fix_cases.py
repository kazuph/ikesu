#!/usr/bin/env python3
"""FIX-1 navigator scenarios using real disposable servers and raw key bytes."""
import json
from pathlib import Path
import sys
out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
def send(text): return 'send '+json.dumps(text)+'\nsleep 200ms\n'
def save(name): return f'screen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'
source=Path(__file__).resolve().parent
body=send('python3 '+str(source/'stage6b_fix_fixture.py')+'\r')+'sleep 2s\n'
body+=send('\x14o\x1b[H ')+save('folded')
body+=send('/NEEDLE_A')+save('search')+send('\x1b')+send('a')+save('search-cleared')
body+=send('b')+save('state')+send('\x7f')+save('state-cleared')
body+=send('/NEEDLE_B')+send('\x0e')+save('select-target')+send('\r')
body+=send('python3 '+str(source/'stage6b_fix_fixture.py')+' focus\r')+save('selected-pane')
# The shell query above saves the actual focused pane through the public API.
(out/'fix1-fold.steps').write_text(body+'kill\n')
body=send('python3 '+str(source/'stage6b_fixture.py')+'\r')+'sleep 2s\n'+send('\x14o')
# Put the selection in the middle via the same real half-page command.
body+=send('\x04')+save('middle')
for name,key in [('normal-n','\x0e'),('normal-p','\x10'),('normal-shift-d','\x1b[100;6u'),('normal-alt-d','\x1b[100;7u'),('normal-shift-u','\x1b[117;6u'),('normal-alt-u','\x1b[117;7u')]:
 body+=send(key)+save(name)
body+=send('/pane')+save('search-middle')
for name,key in [('search-home','\x1b[H'),('search-end','\x1b[F'),('search-shift-n','\x1b[110;6u'),('search-shift-p','\x1b[112;6u'),('search-alt-n','\x1b[110;7u'),('search-alt-p','\x1b[112;7u')]:
 body+=send(key)+save(name)
body+=send('\x0e')+save('search-next')+send('\x10')+save('search-prev')+send('\x1b')
body+=send('a')+send('\x1b[H')+save('normal-home')+send('\x1b[F')+save('normal-end')
body+=send('\x15')+save('normal-up')+send('\x04')+save('normal-down')
(out/'fix1-keys.steps').write_text(body+'kill\n')
