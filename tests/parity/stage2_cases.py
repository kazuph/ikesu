#!/usr/bin/env python3
"""Generate stage-two identical-input procedures and explicit config variants."""
import json
from pathlib import Path
import sys
root=Path(sys.argv[1]); root.mkdir(parents=True,exist_ok=True)
user=Path(sys.argv[2]).read_text()
cases=[]
def add(name, steps, keys=''):
    config=user.replace('[keys]\n','[keys]\n'+keys+'\n',1)
    (root/(name+'.toml')).write_text(config)
    (root/(name+'.steps')).write_text(steps+'\nkill\n')
    cases.append(name)
def send(s): return 'send '+json.dumps(s)+'\nsleep 300ms\n'
def screen(s): return f'screen @OUT@/{s}.txt\ncells @OUT@/{s}.json\n'
for mode in ('enter','r','direct'):
    key={'enter':'\r','r':'r','direct':'\x1b[114;9u'}[mode]
    add('resize-'+mode,send('\x14v')+send('\x14r')+screen('before')+send(key)+send('stage2_marker')+screen('after'),'resize_mode=["prefix+r","cmd+r"]')
# Plain text is separated from the mode-changing key: no first-character ambiguity.
for key,name in [('\t','tab'),('z','zoom'),('v','split'),('n','next-tab'),('h','direction')]:
    add('navigate-'+name,send('\x14v')+send('\x14w')+send(key)+screen('action')+send('abcdef')+screen('after'))
add('navigate-custom',send('\x14g')+screen('normal')+send('\x14w')+send('g')+screen('navigate'))
for name,keys in [('invalid','help="not-a-key"'),('printable','help="a"'),('conflict','help="cmd+y"\nsettings="cmd+y"')]:
    add('config-'+name,screen('startup')+send('a')+screen('plain')+send('\x14?')+screen('default')+send('\x1b')+send('\x1b[121;9u')+screen('direct'),keys)
for name,spec,raw in [('command','command+y','\x1b[121;9u'),('meta','meta+y','\x1by'),('hyper','hyper+y','\x1b[121;17u'),('return','prefix+return','\x14\r'),('bs','prefix+bs','\x14\x7f'),('period','prefix+period','\x14.'),('slash','prefix+slash','\x14/'),('backslash','prefix+backslash','\x14\\'),('quote','prefix+quote',"\x14'"),('semicolon','prefix+semicolon','\x14;'),('colon','prefix+colon','\x14:')]:
    add('alias-'+name,screen('startup')+send(raw)+screen('after'),'help='+json.dumps(spec))
add('shift-slash',send('\x14')+send('\x1b[47:63;2u')+screen('kitty')+send('\x1b')+send('\x14?')+screen('plain'))
add('shift-indexed',send('\x14N')+send('\x1b')+send('\x1b[33;9u')+screen('symbol')+send('\x1b[49;10u')+screen('base'),'switch_workspace="cmd+shift+1..9"'.replace('switch_workspace=', 'focus_agent='))
fixture=send('python3 @FIXTURES@/stage2_fixture.py\r')+'waittext 5s STAGE2_FIXTURE_READY\n'
for key,name in [('W','rename'),('D','close'),('G','worktree'),('1','number')]:
    add('workspace-'+name,fixture+send('\x14w')+send('\x1b[B')+screen('selected')+send(key)+screen('after'))
(root/'index.json').write_text(json.dumps(cases,indent=2)+'\n')
