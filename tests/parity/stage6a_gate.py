#!/usr/bin/env python3
"""Assert stage6a behavior from real API replies, PTY bytes and cell geometry."""
import argparse
import json
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('root',type=Path);p.add_argument('--only',nargs='*');a=p.parse_args()
root=a.root
results={}
def runtime(case,product):
 return json.loads((root/case/product/'runtime-json.json').read_text())
def actions(case,product):
 return json.loads(runtime(case,product)['home/stage6a-actions.json'])
def cells(case,product,name):
 return json.loads((root/case/product/(name+'.json')).read_text())
def text(case,product,name):
 return (root/case/product/(name+'.txt')).read_text()
def boxes(frame):
 grid=frame['Cells'];found=[]
 for y,row in enumerate(grid):
  for x,c in enumerate(row):
   if c['Content']!='┌':continue
   for yy in range(y+1,len(grid)):
    if grid[yy][x]['Content']=='└':
     ends=[xx for xx in range(x+1,len(row)) if row[xx]['Content']=='┐' and grid[yy][xx]['Content']=='┘']
     if ends:found.append((x,y,ends[0]-x+1,yy-y+1))
     break
 return found

def popup_ownership():
 for case in ['popup-duplicate','popup-cross']:
  for product in ['herdr','gtx']:
   entries=actions(case,product)
   if case=='popup-duplicate':
    assert entries[1]['reply']['error']=={'code':'command_failed','message':'popup already open'}
   else:
    close=next(x for x in entries if x['method']=='popup.close')
    assert close['reply']['error']=={'code':'popup_not_open','message':'no popup is open'}
   records=runtime(case,product)['home/popup-received.json']
   received=b''.join(bytes.fromhex(json.loads(line)['hex']) for line in records.splitlines())
   assert received==b'\x1bZ',(case,product,received)
   assert 'PROBE_READY' in text(case,product,'escape')
 return 'Both errors exact; original popup receives Esc and Z after rejected operation.'

def menu():
 for product in ['herdr','gtx']:
  frame=cells('menu-short',product,'last')
  assert (55,0,25,10) in boxes(frame),(product,boxes(frame))
  assert 'Close pane' in text('menu-short',product,'last')
 h=cells('menu-short','herdr','last');g=cells('menu-short','gtx','last')
 assert [r[55:80] for r in h['Cells']]==[r[55:80] for r in g['Cells']],'menu cells differ'
 return '80x10: complete menu box and selected tail match every cell/style.'

def name():
 for shot,expected in [('normal',(52,19,56,7)),('width-56',(2,1,52,5)),('width-20',(2,1,16,5))]:
  for product in ['herdr','gtx']:
   f=cells('name-narrow',product,shot)
   assert expected in boxes(f),(product,shot,boxes(f))
   if shot!='normal':
    assert 'save' not in text('name-narrow',product,shot)
    assert 'rename workspace' not in text('name-narrow',product,shot)
 for shot in ['normal-click','click-56','click-20']:
  left=boxes(cells('name-narrow','herdr',shot))
  right=boxes(cells('name-narrow','gtx',shot))
  assert left==right,(shot,left,right)
 return 'Normal/narrow frame coordinates, hidden controls and identical click outcomes match.'

def sizes():
 for width in [3,5,6]:
  case=f'popup-size-{width}'
  for product in ['herdr','gtx']:
   size=json.loads(runtime(case,product)[f'home/size-{width}.json'])
   assert size=={'cols':4,'rows':2},(case,product,size)
   assert any(b[2:]==(6,4) for b in boxes(cells(case,product,'open')))
 return 'All 3 requested widths use 6x4 outer box and real 4x2 PTY.'

def worktrees():
 for product in ['herdr','gtx']:
  for shot in ['dialog','tail','search','last']:
   assert (32,9,96,26) in boxes(cells('worktree-list',product,shot))
  initial=text('worktree-list',product,'dialog')
  assert 'main' in initial and 'open' in initial and '/fixture-repo' in initial
  tail=text('worktree-list',product,'tail')
  assert 'branch-11' in tail and 'branch-00' not in tail
  filtered=text('worktree-list',product,'search')
  assert '2/13 checkouts' in filtered and 'branch-10' in filtered and 'branch-11' in filtered
  assert 'branch-09' not in filtered
  assert '› branch-11' in text('worktree-list',product,'last')
 return 'Real paths/open state, full tail scrolling, 2/13 filtered count and selected final match.'

def create():
 for product in ['herdr','gtx']:
  assert (46,16,68,12) in boxes(cells('worktree-create',product,'failed'))
  lines=text('worktree-create',product,'failed').splitlines()
  assert 'main' in lines[19] and 'checkout' in lines[20] and '/worktrees/fixture-repo/main' in lines[21]
  assert 'Preparing worktree' in lines[22] and 'already used' in lines[23] and '/fixture-repo' in lines[24]
  assert 'create and open' in lines[26] and 'esc cancel' in lines[26]
 return 'Branch, destination, wrapped Git error and retry/cancel controls remain together.'

def delete():
 for product in ['herdr','gtx']:
  assert (44,17,72,10) in boxes(cells('worktree-delete',product,'dialog'))
  t=text('worktree-delete',product,'dialog')
  for value in ['This removes the checkout folder:','/checkout-00','The branch is not deleted.','workspace will close.','remove','esc cancel']:
   assert value in t
 return 'Unconfirmed deletion explains folder removal, retained branch and workspace closure.'

def group():
 for product in ['herdr','gtx']:
  t=text('worktree-group',product,'dialog')
  assert 'Close worktree group?' in t and 'fixture-repo — 2 workspaces, 2 panes' in t
  remaining=actions('worktree-group',product)[-1]['reply']['result']['workspaces']
  assert len(remaining)==1 and remaining[0]['label']=='parity'
 return 'Same target counts; confirming closes the group and retains the unrelated workspace.'

def configured_popup():
 import re
 for product in ['herdr','gtx']:
  assert any(b[2:]==(107,34) for b in boxes(cells('popup-user',product,'open')))
  assert re.search(r'│32 104\s*│',text('popup-user',product,'size'))
  assert not any(b[2:]==(107,34) for b in boxes(cells('popup-user',product,'escape')))
 return 'User ctrl+t,t opens 80% popup, 107x34 frame/104x32 PTY; shell Esc closes it.'

checks={'popup-ownership':popup_ownership,'menu':menu,'name':name,'sizes':sizes,
        'worktree-list':worktrees,'worktree-create':create,'worktree-delete':delete,
        'worktree-group':group,'configured-popup':configured_popup}
for key,check in checks.items():
 if a.only and key not in a.only:continue
 try:results[key]={'status':'PASS','evidence':check()}
 except Exception as e:results[key]={'status':'FAIL','error':repr(e)}
(root/'stage6a-gate.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(results,ensure_ascii=False,indent=2))
raise SystemExit(any(x['status']!='PASS' for x in results.values()))
