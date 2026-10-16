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
add('navigate-custom',send('\x14g')+screen('normal')+send('\x14w')+screen('navigate-open')+send('g')+screen('navigate'))
for name,keys in [('invalid','help="not-a-key"'),('printable','help="a"'),('conflict','help="cmd+y"\nsettings="cmd+y"')]:
    add('config-'+name,screen('startup')+send('a')+screen('plain')+send('\x14?')+screen('default')+send('\x1b')+send('\x1b[121;9u')+screen('direct'),keys)
for name,spec,raw in [('command','command+y','\x1b[121;9u'),('meta','meta+y','\x1by'),('hyper','hyper+y','\x1b[121;17u'),('return','prefix+return','\x14\r'),('bs','prefix+bs','\x14\x7f'),('period','prefix+period','\x14.'),('slash','prefix+slash','\x14/'),('backslash','prefix+backslash','\x14\\'),('quote','prefix+quote',"\x14'"),('semicolon','prefix+semicolon','\x14;'),('colon','prefix+colon','\x14:')]:
    add('alias-'+name,screen('startup')+send(raw)+screen('after'),'help='+json.dumps(spec))
add('shift-slash',send('\x14')+send('\x1b[47:63;2u')+screen('kitty')+send('\x1b')+send('\x14?')+screen('plain'))

fixture=send('python3 @FIXTURES@/stage2_fixture.py\r')+'waittext 5s STAGE2_FIXTURE_READY\n'
for key,name in [('W','rename'),('D','close'),('G','worktree'),('1','number')]:
    add('workspace-'+name,fixture+send('\x14w')+send('\x1b[B')+screen('selected')+send(key)+screen('after'))
for key,name in [('\t','tab'),('h','direction'),('n','next'),('z','zoom')]:
    add('exit-detail-'+name,send('\x14v')+send('\x14w')+send(key)+screen('action')+send('u')+screen('letter-u')+send('b')+screen('letter-b')+send('marker')+screen('after'))
indexed=send('python3 @FIXTURES@/stage2_fixture.py indexed\r')+'waittext 5s STAGE2_FIXTURE_READY\n'
for i,symbol in enumerate('!@#$%^&*('):
    indexed+=send('\x1b['+str(ord(symbol))+';9u')+screen('symbol-'+str(i+1))
    indexed+=send('\x1b['+str(ord('1')+i)+';10u')+screen('base-'+str(i+1))
add('indexed-workspace',indexed)
p=root/'indexed-workspace.toml';s=p.read_text();old='switch_workspace = "cmd+1..9"';assert old in s;p.write_text(s.replace(old,'switch_workspace = "cmd+shift+1..9"'))
for name,keys in [('array','help=["not-a-key","cmd+y"]'),('default-conflict','help="prefix+s"'),('reverse-conflict','settings="cmd+y"\nhelp="cmd+y"')]:
    add('config-'+name,screen('startup')+send('\x14s')+screen('default')+send('\x1b')+send('\x1b[121;9u')+screen('direct'),keys)

direct=fixture
for raw,name in [('\x1b[49;9u','one'),('\x1b[51;9u','three'),('\x1b[106;9u','next'),('\x1b[107;9u','previous'),('\x1b[91;9u','history-back'),('\x1b[93;9u','history-forward')]:
    direct+=send(raw)+screen(name)
add('user-direct',direct)
for key,name in [('W','rename'),('D','close')]:
    add('workspace-cancel-'+name,fixture+send('\x14w')+send('\x1b[B')+screen('selected')+send(key)+screen('dialog')+send('\x1b')+screen('cancelled'))
add('navigate-direction-remapped',send('\x14v')+screen('split')+send('\x14w')+send('u')+screen('action'), 'focus_pane_left="cmd+y"\nnavigate_pane_left="u"')
# First-review reproductions keep both config orders and both terminal encodings.
for direct in (False, True):
    prefix='' if direct else '\x14'
    modifier='9' if direct else '1'
    shifted='10' if direct else '2'
    for reverse in (False, True):
        bindings=[('help', ('cmd+' if direct else 'prefix+')+'?'),('settings', ('cmd+shift+' if direct else 'prefix+shift+')+'?')]
        if reverse: bindings.reverse()
        keys='\n'.join(name+'='+json.dumps(value) for name,value in bindings)
        plain='\x1b[63;'+modifier+'u' if direct else '?'
        steps=screen('startup')+send(prefix+plain)+screen('plain')+send('\x1b')+send(prefix+'\x1b[47:63;'+shifted+'u')+screen('kitty')
        add('review-conflict-'+('direct' if direct else 'prefix')+('-reverse' if reverse else ''),steps,keys)
for name,spec,base,symbol,mod,prefix in [('slash-base','prefix+shift+slash',47,63,2,'\x14'),('slash-symbol','prefix+?',47,63,2,'\x14'),('digit-base','cmd+shift+1',49,33,10,''),('digit-symbol','cmd+!',49,33,10,'')]:
    steps=screen('startup')
    for frame,code in [('base',str(base)),('alternate',str(base)+':'+str(symbol)),('symbol',str(symbol))]:
        steps+=send(prefix+'\x1b['+code+';'+str(mod)+'u')+screen(frame)+send('\x1b')
    add('review-'+name,steps,'help='+json.dumps(spec))
unchanged=send('python3 @FIXTURES@/stage2_fixture.py indexed\r')+'waittext 5s STAGE2_FIXTURE_READY\n'
for i in range(1,10):
    other=9 if i==1 else i-1
    unchanged+=send('\x1b['+str(ord('0')+other)+';9u')+screen('before-number-'+str(i))
    unchanged+=send('\x1b['+str(ord('0')+i)+';9u')+screen('number-'+str(i))
add('user-all-numbers',unchanged)
(root/'index.json').write_text(json.dumps(cases,indent=2)+'\n')
