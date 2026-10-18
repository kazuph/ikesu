#!/usr/bin/env python3
"""Identical isolated keyboard procedures for stage 6b."""
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)

def send(text):
    return 'send '+json.dumps(text, ensure_ascii=False)+'\nsleep 200ms\n'

def save(name):
    return f'screen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'

cases = {}
body = ''
for name, key in [('ctrl-u', '\x15'), ('cmd-backspace', '\x1b[127;9u'),
                  ('ctrl-backspace', '\x1b[127;5u'), ('alt-backspace', '\x1b\x7f'),
                  ('ctrl-h', '\x08'), ('ctrl-w', '\x17'), ('backspace', '\x7f'), ('ctrl-c', '\x03')]:
    body += send('\x14c')+send('日本語 alpha-beta  ')+send(key)+save(name)+send('\x1b')
body += send('\x14c')+send('\x7f')+save('default-backspace')
cases['k15-name'] = body
cases['k16-paste'] = (send('\x14c')+send('\x1b[200~貼付 alpha\x1b[201~')+save('name')+send('\x1b')+
    send('\x14o/')+send('\x1b[200~pane\x1b[201~')+save('navigator')+send('\x1b')+send('\x1b')+
    send("printf 'alpha beta\\n'\r")+send('\x14[/')+send('\x1b[200~alpha\t\x01beta\x1b[201~')+save('copy-search')+
    send('\x1b')+send('\x1b[200~DO_NOT_DELIVER\x1b[201~')+save('copy-no-search'))
body = send('\x14s')+save('open')
for name, key in [('j','j'), ('k','k'), ('l','l'), ('down','\x1b[B'), ('space',' '), ('enter','\r'), ('h','h')]:
    body += send(key)+save(name)
body += send('\x1b')+send('\x14s')+save('reopen')
cases['k17-settings'] = body
body = send('\x14s')
for i in range(6):
    body += save(f'right-{i}')+send('\x1b[C')
body += send('\x1b')+send('\x14s')+send('a')+save('label-shortcut')+send('e')+save('experiments-shortcut')
cases['k18-settings'] = body
body = send('\x14?')+save('open')
for name, key in [('page','\x1b[6~'), ('end','\x1b[F'), ('home','\x1b[H'), ('q','q'), ('question','?')]:
    body += send(key)+save(name)
body += 'resize 160 18\nsleep 200ms\n'+send('\x14?')+save('short-open')+send('\x1b[6~')+save('short-page')
cases['k21-help'] = body
cases['k22-help'] = send('\x14?')+save('open')+''.join(send('\x1b[6~')+save(f'page-{i}') for i in range(8))
cases['k22-user'] = cases['k22-help']+send('\x1b')+send('\x14g')+save('custom-pane')+send('\x14t')+save('custom-popup')
cases['k22-nav'] = cases['k22-help']
cases['k22-chrome'] = send('\x14c')+send('\r')+save('tabs')
body = send('\x14o')+save('open')
for name, key in [('down','\x04'), ('up','\x15'), ('end','\x1b[F'), ('home','\x1b[H'), ('G','G'), ('space',' '), ('space-again',' ')]:
    body += send(key)+save(name)
body += send('\x1b[H')+send(' ')+save('collapsed')+send(' ')+save('expanded')
body += send('/pane')+send('\x1b[H')+send('\x0e')+save('search-next')+send('\x10')+save('search-previous')
cases['k19-navigator'] = body
cases['k20-navigator'] = (send('\x14o/bogus')+send('\x1b')+send('a')+save('all')+
    send('b/')+send('pane')+save('filter-search')+send('\x1b')+send('b')+save('filtered')+send('\x7f')+save('backspace'))
body = ''
for i, text in enumerate(['one_two', 'alpha---  ', '日本語 abc', 'alpha / /  ', 'alpha🙂🙂  ']):
    body += send('\x14c')+send(text)+send('\x17')+save(f'word-{i}')+send('\x1b')
cases['k15-word-boundaries'] = body
for keyname, key in [('space',' '), ('enter','\r')]:
    cases['k17-'+keyname] = send('\x14s')+send('l')+save('open')+send(key)+save('first')+send(key)+save('second')+send('\x1b')+send('\x14s')+save('reopen')
    cases['k17-toast-'+keyname] = send('\x14s')+send('ll')+send('k')+save('open')+send(key)+save('applied')+send('\x1b')+send('\x14s')+save('reopen')
cases['k16-copy'] = (send("printf 'alpha beta\\n'\r")+send('\x14[')+save('copy')+send('/')+save('search-open')+
    send('\x1b[200~alpha\x1b[201~')+save('plain')+send('\x1b[200~\t\x01beta\x1b[201~')+save('controls'))
recorder = send('python3 '+str(Path(__file__).resolve().with_name('stage6b_paste_recorder.py'))+'\r')+'sleep 400ms\n'
cases['k16-no-pane'] = (recorder+send('\x14[')+send('\x1b[200~FORBIDDEN_COPY\x1b[201~')+save('copy')+
    send('q')+send('\x14')+send('\x1b[200~FORBIDDEN_PREFIX\x1b[201~')+save('prefix')+
    send('\x1b')+send('\x14w')+send('\x1b[200~FORBIDDEN_NAVIGATE\x1b[201~')+save('navigate')+
    send('\x1b')+send('DELIVERY_SENTINEL')+save('delivered'))
cases['k16-native-denied'] = send('\x14c')+send('KEEP')+send('\x1b[118;9u')+save('command-v')+send('\x16')+save('control-v')
fixture = send('python3 '+str(Path(__file__).resolve().with_name('stage6b_fixture.py'))+'\r')+'sleep 2s\n'
cases['k19-navigator'] = fixture+cases['k19-navigator']
tabs_fixture = send('python3 '+str(Path(__file__).resolve().with_name('stage6b_fixture.py'))+' tabs\r')+'sleep 2s\n'
cases['k15-multidigit'] = tabs_fixture+send('\x14c')+save('initial')+send('\x7f')+save('cleared')
cases['k17-theme-edges'] = send('\x14s')+send('kkkk')+save('up-edge')+send('j')+save('down-edge')+send(' ')+save('space')
cases['k17-toast-edges'] = send('\x14s')+send('ll')+send('j')+save('down-edge')+send('k')+save('up-edge')+send('kkkk')+save('top-edge')
for name, body in cases.items():
    (out/(name+'.steps')).write_text(body+'kill\n')
