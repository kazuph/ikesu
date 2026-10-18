#!/usr/bin/env python3
"""Assert stage-6b observations; report each check without masking failures."""
import json
import re
import sys
import tomllib
from pathlib import Path

root = Path(sys.argv[1])
proof = {}

def screen(case, product, phase):
    return (root/case/product/(phase+'.txt')).read_text().splitlines()

def name(case, product, phase):
    lines = screen(case, product, phase)
    title = next(i for i, x in enumerate(lines) if '│new tab' in x)
    return lines[title+2].split('│')[2].strip()

def check(label, fn):
    try:
        proof[label] = {'status': 'PASS', 'evidence': fn()}
    except Exception as exc:
        proof[label] = {'status': 'FAIL', 'error': str(exc)}

def names():
    expected = {'ctrl-u':'', 'cmd-backspace':'', 'ctrl-backspace':'日本語 alpha-',
                'alt-backspace':'日本語 alpha-', 'ctrl-h':'日本語 alpha-', 'ctrl-w':'日本語 alpha-',
                'ctrl-c':'', 'default-backspace':'', 'backspace':'日本語 alpha-beta'}
    result = {}
    for phase, want in expected.items():
        values = [name('k15-name', p, phase) for p in ('herdr','gtx')]
        assert values == [want, want], (phase, values, want)
        result[phase] = values
    for i, want in enumerate(['', 'alpha', '日本語', 'alpha /', 'alpha']):
        values = [name('k15-word-boundaries', p, f'word-{i}') for p in ('herdr','gtx')]
        assert values == [want, want], (i, values, want)
        result[f'word-{i}'] = values
    initial=[name('k15-multidigit',p,'initial') for p in ('herdr','gtx')]
    assert initial[0]==initial[1] and initial[0].isdigit() and len(initial[0])>1,initial
    cleared=[name('k15-multidigit',p,'cleared') for p in ('herdr','gtx')]
    assert cleared==['',''],cleared
    result['multidigit']={'initial':initial,'cleared':cleared}
    return result

def settings():
    result = {}
    for case in ('k17-settings','k17-enter','k17-space','k17-toast-enter','k17-toast-space'):
        for p in ('herdr','gtx'):
            phases = ('space','enter') if case == 'k17-settings' else ('applied',) if 'toast' in case else ('first','second')
            for phase in phases:
                assert any('│ settings' in x for x in screen(case,p,phase)), (case,p,phase,'closed')
            reopen = screen(case,p,'reopen')
            assert any('▸ tokyo-night' in x for x in reopen), (case,p,'reopen not Theme')
            configs = json.loads((root/case/p/'config-files.json').read_text())
            c = tomllib.loads(configs[f'config/{p}/config.toml'])
            if 'toast' in case:
                assert c['ui']['toast']['delivery'] == 'terminal', (case,p,c)
            else:
                assert c['ui']['sound']['enabled'] == (case == 'k17-settings'), (case,p,c)
            result[case+'/'+p] = c['ui']
    for p in ('herdr','gtx'):
        for phase,theme in [('up-edge','catppuccin'),('down-edge','catppuccin-latte'),('space','catppuccin-latte')]:
            assert any('▸ '+theme+' ✓' in x for x in screen('k17-theme-edges',p,phase)),(p,phase)
            assert any('│ settings' in x for x in screen('k17-theme-edges',p,phase)),(p,phase,'closed')
        configs=json.loads((root/'k17-theme-edges'/p/'config-files.json').read_text())
        c=tomllib.loads(configs[f'config/{p}/config.toml'])
        assert c['theme']['name']=='tokyo-night',(p,'Theme Space persisted preview')
        for phase,selected in [('down-edge',3),('up-edge',2),('top-edge',0)]:
            cells=json.loads((root/'k17-toast-edges'/p/(phase+'.json')).read_text())['Cells']
            rows=[i for i,line in enumerate(screen('k17-toast-edges',p,phase)) if 'notification popups:' in line]
            highlighted=[n for n,y in enumerate(rows) if cells[y][50]['Style']['Bg'] is not None]
            assert highlighted==[selected],(p,phase,highlighted,selected)
            result[p+'/'+phase]={'selected':highlighted}
    return result

def selected(case,p,phase):
    lines=screen(case,p,phase)
    row=next(x[11:146].strip() for x in lines[7:38] if '→' in x[11:146])
    base=json.loads((root/case/p/'fixture.json').read_text())['result']['root_pane']['pane_id']
    start=int(re.search(r'\d+',base)[0])
    return re.sub(r'pane (\d+)', lambda m: 'pane '+str(int(m[1])-start+1), row)

def navigation():
    result={}
    for phase in ('open','down','up','end','home','G','space','space-again','collapsed','expanded','search-next','search-previous'):
        values=[selected('k19-navigator',p,phase) for p in ('herdr','gtx')]
        assert values[0] == values[1], (phase,values)
        result[phase]=values
    assert 'nav-07' in result['down'][0], result['down']
    assert result['collapsed'][0].startswith('▸'),result['collapsed']
    assert result['expanded'][0].startswith('▾'),result['expanded']
    for phase in ('all','filter-search','backspace'):
        for p in ('herdr','gtx'):
            lines=screen('k20-navigator',p,phase)
            assert any('pane ' in x for x in lines[7:38]), (p,phase,'empty list')
            assert 'blocked' not in lines[5] and 'bogus' not in lines[5],(p,phase,lines[5])
    return result

def help_keys():
    result={}
    for p in ('herdr','gtx'):
        for phase in ('q','home','end','page','short-page'):
            assert any('keybinds' in x for x in screen('k21-help',p,phase)),(p,phase)
        assert not any('keybinds' in x for x in screen('k21-help',p,'question')),(p,'? did not close')
        for phase in ('open','home','end','page','short-page'):
            lines=screen('k21-help',p,phase)
            title=next(i for i,x in enumerate(lines) if '│keybinds' in x)
            result[p+'/'+phase]=lines[title+4][42:116].strip().rstrip('│▐▕ ')
    for phase in ('open','home','end','page','short-page'):
        assert result['herdr/'+phase]==result['gtx/'+phase],(phase,result)
    return result

def effective_keys():
    result={}
    for case in ('k22-user','k22-nav'):
        for p in ('herdr','gtx'):
            lines=sum((screen(case,p,phase) for phase in ('open','page-0','page-1','page-5','page-6','page-7')),[])
            for text,want in [('session navigator','unset' if case=='k22-user' else 'prefix+o'),
                              ('workspace list','up / down' if case=='k22-user' else 'u / d'),
                              ('move focus','h / j / k / l / left / right' if case=='k22-user' else 'a / s / w / f / left / right'),
                              ('open scratch terminal','prefix+t')]:
                row=next(x for x in lines if text in x)
                assert want in row,(case,p,text,row)
                result[case+'/'+p+'/'+text]=row.strip()
    return result

def paste():
    result={}
    for p in ('herdr','gtx'):
        assert name('k16-paste',p,'name') == '貼付 alpha',(p,'default not replaced')
        assert '/ pane' in screen('k16-paste',p,'navigator')[5],(p,'navigator paste dropped')
        for phase in ('plain','controls'):
            assert screen('k16-copy',p,phase)[-1].strip() == 'COPY  /█  enter search  esc cancel',(p,phase)
        runtime=json.loads((root/'k16-no-pane'/p/'runtime-json.json').read_text())
        chunks=json.loads(runtime['home/stage6b-paste.json'])
        received=b''.join(bytes.fromhex(x) for x in chunks)
        assert b'FORBIDDEN_COPY' not in received and b'FORBIDDEN_NAVIGATE' not in received,(p,received)
        assert b'FORBIDDEN_PREFIX' in received,(p,'0.2.57 forwards prefix paste',received)
        assert b'DELIVERY_SENTINEL' in received,(p,'recorder did not receive control input',received)
        result[p]={'shell_input_hex':received.hex()}
        for phase in ('command-v','control-v'):
            assert name('k16-native-denied',p,phase) == 'KEEP',(p,phase,'key leaked')
    result['native_nonempty_clipboard']='NOT_RUN: owner pasteboard is intentionally denied; private-file dependency tests cover insertion and cancellation'
    return result

def configured_chrome():
    result={}
    for p in ('herdr','gtx'):
        folder=root/'k22-user'/p
        fixture=json.loads((folder/'fixture.json').read_text())['result']['root_pane']['pane_id']
        next_pane=int(re.search(r'\d+',fixture)[0])+1
        assert f'ZOOM %{next_pane} ' in screen('k22-user',p,'custom-pane')[1],(p,'custom pane key did not run')
        lines=screen('k22-user',p,'custom-popup')
        y=next(i for i,x in enumerate(lines) if '┌popup' in x)
        x=lines[y].index('┌popup'); right=lines[y].index('┐',x)
        bottom=next(i for i in range(y+1,len(lines)) if len(lines[i])>x and lines[i][x]=='└')
        parent_x=lines[1].index('┌'); parent_right=lines[1].rindex('┐')
        parent_bottom=next(i for i in range(2,len(lines)) if len(lines[i])>parent_x and lines[i][parent_x]=='└')
        expected=(int((parent_right-parent_x+1)*.8),int(parent_bottom*.8))
        actual=(right-x+1,bottom-y+1)
        assert actual==expected,(p,actual,expected)
        view=json.loads((root/'k22-chrome'/p/'tabs.json').read_text())
        reference=json.loads((root/'k22-chrome'/'reference.json').read_text())
        config=tomllib.loads(reference['config'])
        assert config['keys']['prefix']=='ctrl+t' and config['ui']['show_tab_bar'] and config['theme']['name']=='tokyo-night'
        counts={}
        cells=[cell for row in view['Cells'] for cell in row]
        for token,value in config['theme']['custom'].items():
            if value=='transparent':
                assert view['Cells'][0][0]['Style']['Bg'] is None,(p,'sidebar is opaque')
                continue
            rgb=tuple(int(value[i:i+2],16) for i in (1,3,5))
            n=sum(1 for cell in cells for field in ('Fg','Bg') if cell['Style'][field] and tuple(cell['Style'][field][k] for k in ('R','G','B'))==rgb)
            assert n>0,(p,token,'configured color absent')
            counts[token]=n
        assert '+' in screen('k22-chrome',p,'tabs')[0],(p,'tab bar absent')
        result[p]={'popup':actual,'custom_color_cells':counts}
    return result

check('K15-name-deletion',names)
check('K16-paste-routing',paste)
check('K17-settings',settings)
check('K19-K20-navigator',navigation)
check('K21-help',help_keys)
check('K22-effective-keys',effective_keys)
check('configured-chrome',configured_chrome)
(root/'gate.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(proof,ensure_ascii=False,indent=2))
raise SystemExit(any(x['status']!='PASS' for x in proof.values()))
