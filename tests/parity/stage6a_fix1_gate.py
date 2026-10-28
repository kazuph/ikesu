#!/usr/bin/env python3
"""Assert exact notification bytes, responses and worktree geometry for FIX-1."""
import json
from pathlib import Path
import sys
root=Path(sys.argv[1]);proof={}
def load(case,product,name):return json.loads((root/case/product/(name+'.json')).read_text())
def actions(case,product):return json.loads(load(case,product,'runtime-json')['home/stage6a-actions.json'])
def notification_bytes(name,title,body):
    end=b'\x1b\\';title=title.encode();body=body.encode()
    if 'kitty' in name:
        result=b'\x1b]99;i=1:d=0;'+title+end+b'\x1b]99;i=1:p=body;'+body+end if body else b'\x1b]99;;'+title+end
    else:result=b'\x1b]9;'+title+(b': '+body if body else b'')+end
    if name.endswith('tmux'):result=b'\x1bPtmux;'+result.replace(b'\x1b',b'\x1b\x1b')+end
    return result
for name in ['ghostty','iterm','wezterm','kitty','term-ghostty','term-kitty','term-wezterm','tmux','kitty-tmux','unknown']:
    case='terminal-'+name
    replies=[]
    for who in ['herdr','ikesu']:
        entries=actions(case,who);r=[e['reply'] for e in entries];assert len(r)==8,(case,who,len(r))
        for i in [0,2,3,4,6,7]:assert r[i]['result']=={'type':'notification_show','shown':True,'reason':'shown'},(case,who,i,r[i])
        assert r[1]['result']=={'type':'notification_show','shown':False,'reason':'rate_limited'},(case,who,r[1])
        raw=(root/case/who/'terminal.raw').read_bytes()
        if name=='unknown':
            assert b'\x1b]9;' not in raw and b'\x1b]99;' not in raw,(case,who,'unknown terminal emitted notification')
        else:
            for title,body in [('日本語','本文'),('日本語',''),('A B','C D'),('日'*80,'語'*240),('日'*79,'語'*239)]:
                expected=notification_bytes(name,title,body);assert expected in raw,(case,who,len(title),len(body),'missing full byte sequence')
        assert r[5]['error']=={'code':'invalid_params','message':'notification title is empty'},(case,who,r[5])
        replies.append(r)
    assert replies[0]==replies[1],case
    proof[case]={'status':'PASS','comparison':'complete API replies and full terminal byte sequences, including Japanese length limits before trim'}

def box(frame,height):
    x,y,w,h=32,(45-height)//2,96,height;c=frame['Cells']
    assert [c[y][x]['Content'],c[y][x+w-1]['Content'],c[y+h-1][x]['Content'],c[y+h-1][x+w-1]['Content']]==['┌','┐','└','┘']
    return x,y,w,h
for count,height in [(1,12),(2,12),(3,13),(13,26)]:
    case=f'worktree-{count}';last='main' if count==1 else f'branch-{count-2:02}'
    for who in ['herdr','ikesu']:
        for shot in ['initial','last','filtered','filtered-last']:box(load(case,who,shot),height)
        for shot in ['last','filtered-last']:
            assert '› '+last in (root/case/who/(shot+'.txt')).read_text(),(case,who,shot,last)
        clicked=(root/case/who/'clicked.txt').read_text();assert 'open worktree' not in clicked,(case,who,'click did not open selected checkout')
        # The final API reply records the checkout actually opened by the click.
        final=actions(case,who)[-1]['reply']['result']['workspaces']
        assert any(w.get('worktree',{}).get('checkout_path','').endswith('/fixture-repo' if count==1 else f'/checkout-{count-2:02}') for w in final),(case,who,final)
        assert ' terminal '+last+' ' in clicked,(case,who,'display did not select clicked checkout',last)
    proof[case]={'status':'PASS','height':height,'comparison':'initial and filtered geometry, last selection, real checkout opened by click'}

for case in ['inapp-rate','inapp-escape']:
    for who in ['herdr','ikesu']:
        entries=actions(case,who)
        assert [e['reply']['result']['reason'] for e in entries]==['shown','busy','shown'],(case,who,entries)
        frame=load(case,who,'after-key');assert frame['Cells'][0][148]['Content']=='┌',(case,who,'key dismissed targetless toast')
    proof[case]={'status':'PASS','comparison':'keys retain targetless toast, busy while visible, shown after three-second expiry'}
cleanups=[]
for path in sorted(root.glob('*/*/cleanup.json')):
    d=json.loads(path.read_text());assert d['exit']==0,(str(path),d)
    assert any(json.loads(x).get('result',{}).get('type')=='ok' for x in d['replies']),(str(path),d)
    cleanups.append(str(path.relative_to(root)))
assert len(cleanups)==32,len(cleanups)
result={'cases':proof,'cleanup_servers':cleanups}
(root/'fix1-gate.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False))
