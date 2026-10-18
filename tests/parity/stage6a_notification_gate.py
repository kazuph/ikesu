#!/usr/bin/env python3
"""Verify A40 real API replies and every toast cell/style; no renderer stand-in."""
import argparse
import json
from pathlib import Path
import re

p=argparse.ArgumentParser();p.add_argument('root',type=Path);a=p.parse_args();root=a.root
proof={'cases':{},'identifier_normalization':[]}
def load(case,product,name):return json.loads((root/case/product/(name+'.json')).read_text())
def entries(case,product):return json.loads(load(case,product,'runtime-json')['home/stage6a-actions.json'])
def crop(frame,box):
 x,y,w,h=box
 return [row[x:x+w] for row in frame['Cells'][y:y+h]]
def explicit(case,box,title='日本語',body='本文'):
 frames={who:load(case,who,'toast') for who in ['herdr','gtx']}
 h,g=[crop(frames[who],box) for who in ['herdr','gtx']]
 assert h==g,(case,'toast cells/styles differ')
 assert h[0][0]['Content']=='┌' and h[-1][-1]['Content']=='┘',(case,'missing frame')
 for who in ['herdr','gtx']:
  reply=entries(case,who)[0]['reply']
  assert reply.get('result')=={'type':'notification_show','shown':True,'reason':'shown'},(case,who,reply)
 proof['cases'][case]={'status':'PASS','box':box,'compared':'all toast cells, width, foreground/background, attributes'}
for kind,height in [('empty',3),('body',4)]:explicit('toast-explicit-'+kind,(148,0,12,height))
explicit('toast-trim',(148,0,12,4))
for position,box in [('top-left',(0,0,12,4)),('top-right',(148,0,12,4)),('bottom-left',(0,41,12,4)),('bottom-right',(148,41,12,4))]:explicit('toast-position-'+position,box)
for who in ['herdr','gtx']:
 e=entries('toast-blank',who)[0]['reply']
 assert e.get('error')=={'code':'invalid_params','message':'notification title is empty'},(who,e)
 e=entries('toast-probe',who)
 assert e[2]['reply']['result']=={'type':'notification_show','shown':True,'reason':'shown'}
 for i in [3,4,8]:assert e[i]['reply']['result']=={'type':'notification_show','shown':False,'reason':'busy'},(who,i,e[i])
 for i in [0,5,6,7]:assert e[i]['reply']['error']['code']=='invalid_request',(who,i,e[i])
proof['cases']['validation_and_busy']={'status':'PASS','note':'Deserializer message text is product-specific; error code/schema rejection match. Empty-title message matches exactly.'}
background=[]
for who in ['herdr','gtx']:
 e=entries('toast-japanese',who)
 pid=next(x['reply']['result']['root_pane']['pane_id'] for x in e if x['method']=='workspace.create')
 frame=load('toast-japanese',who,'toast');c=crop(frame,(144,0,16,4))
 assert c[0][0]['Content']=='┌' and c[3][-1]['Content']=='┘'
 # Only the actual public pane number differs in these isolated products.
 assert c[2][12]['Content']==pid[1:],(who,pid,c[2][12])
 proof['identifier_normalization'].append({'product':who,'pane_id':pid,'cell':[156,2],'original':c[2][12]})
 c[2][12]=dict(c[2][12],Content='<actual pane number>')
 background.append(c)
assert background[0]==background[1],'background toast cells/styles differ beyond actual target identifier'
proof['cases']['background_japanese']={'status':'PASS','box':[144,0,16,4]}
for who in ['herdr','gtx']:
 for shot,visible in [('initial',True),('dismissed',True),('retry',True),('after-eight',False),('after-ten',False),('third',True)]:
  f=load('toast-lifecycle',who,shot)
  assert (f['Cells'][1][149]['Content']=='●')==visible,(who,shot)
 e=entries('toast-lifecycle',who)
 assert [x['reply']['result']['reason'] for x in e]==['shown','busy','shown'],(who,e)
 f=load('toast-background-target',who,'target')
 target=next(x['reply']['result']['root_pane']['pane_id'] for x in entries('toast-background-target',who) if x['method']=='workspace.create')
 header=''.join(c['Content'] for c in f['Cells'][1])
 assert f' %{target[1:]} codex ' in header,(who,target,header)
 selected=[''.join(c['Content'] for c in row) for row in f['Cells'] if row[0]['Content']=='▌']
 assert any('日本語' in row for row in selected),(who,selected)
 assert f['Cells'][1][145]['Content']!='●','toast target navigation did not dismiss'
 f=load('toast-background-expiry',who,'expired')
 assert f['Cells'][1][145]['Content']!='●','background toast did not expire'
proof['cases']['lifecycle_and_target']={'status':'PASS','checks':['targetless click retains toast','busy preserves current toast','expires','new notification after expiry','background target navigation and dismissal']}
tick=int(re.search(r'tickInterval\s*=\s*(\d+)\s*\*\s*time.Millisecond',(Path(__file__).resolve().parents[2]/'internal/tui/tui.go').read_text())[1])
clocks={}
for who in ['herdr','gtx']:
 initial=load('toast-lifecycle',who,'initial');ended=load('toast-lifecycle',who,'after-ten')
 clocks[who]=ended['CellChangedUnixMs'][1][149]-initial['CellChangedUnixMs'][1][149]
assert abs(clocks['herdr']-clocks['gtx'])<=tick,('visible lifetime',clocks,tick)
proof['cases']['explicit_lifetime']={'status':'PASS','measured_ms':clocks,'allowed_difference_ms':tick,'source':'existing renderer tickInterval'}
for path in root.glob('*/herdr/cleanup.json'):
 for who in ['herdr','gtx']:
  cleanup=json.loads(path.with_name('cleanup.json').parent.parent.joinpath(who,'cleanup.json').read_text())
  assert cleanup['exit']==0,(path,who,cleanup)
(root/'stage6a-notification-gate.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(proof,ensure_ascii=False))
