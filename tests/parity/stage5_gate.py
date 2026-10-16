#!/usr/bin/env python3
"""Assert the measured stage-five surfaces; no whole-screen success inference."""
import json
from pathlib import Path
import sys
root=Path(sys.argv[1]); checks=[]
def check(case,claim,ok,detail=None):
    checks.append({'case':case,'claim':claim,'status':'PASS' if ok else 'FAIL','detail':detail})
def load(case,who,name):
    return json.loads((root/case/who/(name+'.json')).read_text())
def region(case,name,x,y,w,h):
    pair=[load(case,who,name) for who in ['herdr','gtx']]
    differences=[]
    for yy in range(y,y+h):
        for xx in range(x,x+w):
            a,b=[p['Cells'][yy][xx] for p in pair]
            if a!=b:differences.append({'x':xx,'y':yy,'herdr':a,'gtx':b})
    check(case,name+' exact region',not differences,differences)
try:
    for case in ['tabs-on','tabs-off']:
        for name in ['created']+[f'select-{i:02}' for i in range(1,16)]:
            region(case,name,26,0,64,1)
        for who in ['herdr','gtx']:
            row=load(case,who,'created')['Cells'][0][26:]
            text=''.join(c['Content'] for c in row)
            check(case,who+' mouse controls',all(c in text for c in '<>+') if case=='tabs-on' else all(c not in text for c in '<>+'),text)
    for case in ['title-ascii','title-wide']:
        region(case,'title',58,1,32,1)
        for who in ['herdr','gtx']:
            row=load(case,who,'title')['Cells'][1][58:]
            check(case,who+' visible ellipsis',any(c['Content']=='…' for c in row))
    for case,xs,y,h,sizes in [('scroll-bordered',[61,98],2,26,[34,33]),('scroll-borderless',[61,99],1,28,[35,37])]:
        for name in ['left-focus','right-focus']:
            for x in xs:region(case,name,x,y,1,h)
        for who in ['herdr','gtx']:
            docs=json.loads((root/case/who/'runtime-json.json').read_text())
            dimensions=[json.loads(v) for k,v in docs.items() if 'stage5-size-' in k]
            check(case,who+' real PTY columns',sorted(d['columns'] for d in dimensions)==sorted(sizes),dimensions)
            for name in ['left-focus','right-focus']:
                cells=load(case,who,name)['Cells']
                check(case,who+' '+name+' scrollbar presence',all(any(cells[yy][x]['Content'] in ['▕','▐'] for yy in range(y,y+h)) for x in xs))
    for width in range(1,6):
        for phase in ['width','click']:
            region('narrow',f'{phase}-{width}',26,29,width,1)
        for who in ['herdr','gtx']:
            doc=load('narrow',who,f'click-{width}')
            # Partial click opens in-layout copy; the same bar becomes PHONE COPY.
            row=''.join(c['Content'] for c in doc['Cells'][29][26:])
            check('narrow',f'{who} width {width} click',row==' PHONE COPY '[:width],row)
    for case in ['empty-ui-server','empty-ui-local']:
        for who in ['herdr','gtx']:
            text=(root/case/who/'empty.txt').read_text()
            check(case,who+' replacement pane and home', '%2 terminal' in text and '2 home' in text)
    for who in ['herdr','gtx']:
        proof=load('empty',who,'close-proof')
        before=proof['before']['result']['workspaces'];after=proof['after']['result']['workspaces']
        panes=proof['panes']['result']['panes']
        check('empty',who+' API replaces identity and focuses home',len(before)==len(after)==1 and before[0]['workspace_id']!=after[0]['workspace_id'] and after[0]['label']=='home' and after[0]['focused'])
        check('empty',who+' API replacement shell cwd',len(panes)==1 and panes[0]['cwd'].startswith('/private/tmp/parity-') and panes[0]['cwd'].endswith('/home'),panes)
    for case in ['startup-empty-server','startup-empty-local']:
        for who in ['herdr','gtx']:
            fixture=load(case,who,'saved-session-fixture')
            check(case,who+' empty save was supplied',bool(fixture) and all(d['workspaces']==[] for d in fixture.values()))
            text=(root/case/who/'startup.txt').read_text()
            check(case,who+' startup creates workspace instead of empty screen','%1 terminal' in text and ('1 home' in text or '1 cwd' in text))
except (OSError,KeyError,IndexError) as error:
    check('collection','required evidence present',False,str(error))
(root/'stage5-gate.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2)+'\n')
failed=[c for c in checks if c['status']!='PASS']
print(f'{len(checks)-len(failed)}/{len(checks)} PASS')
for c in failed:print(c['case'],c['claim'],str(c['detail'])[:400])
raise SystemExit(bool(failed))
