#!/usr/bin/env python3
"""Check FIX-1 real-screen rows, selected panes and unchanged key contexts."""
import json,re,sys
from pathlib import Path
root=Path(sys.argv[1]);proof={}
def lines(case,product,phase):return (root/case/product/(phase+'.txt')).read_text().splitlines()
def body(case,product,phase):return [x[11:146].strip(' │') for x in lines(case,product,phase)[7:38] if any(z in x[11:146] for z in ['▸','▾','├─','└─'])]
def selected(product,phase):
 ls=lines('fix1-keys',product,phase)
 row=next(x[11:146] for x in ls[7:38] if '→' in x[11:146])
 match=re.search(r'nav-\d+|pane \d+|parity',row)
 assert match,(phase,row)
 value=match[0]
 if value.startswith('pane '):
  ref=json.loads((root/'fix1-keys'/product/'fixture.json').read_text())['result']['root_pane']['pane_id']
  value='pane '+str(int(value.split()[1])-int(re.search(r'\d+',ref)[0])+1)
 return value
for product in ['herdr','gtx']:
 counts={}
 for phase,names,arrow in [('folded',[], '▸'),('search',['NEEDLE_A'],'▾'),('search-cleared',[],'▸'),('state',['NEEDLE_A'],'▾'),('state-cleared',[],'▸'),('select-target',['NEEDLE_B'],'▾')]:
  rows=body('fix1-fold',product,phase)
  assert len(rows)==len(names)+1,(product,phase,rows)
  assert arrow in rows[0],(product,phase,rows)
  for name,row in zip(names,rows[1:]):assert name in row,(phase,row,name)
  counts[phase]=rows
 runtime=json.loads((root/'fix1-fold'/product/'runtime-json.json').read_text())
 fixture=json.loads(runtime['home/stage6b-fix-fixture.json'])
 focused=json.loads(runtime['home/stage6b-fix-focus.json'])
 expected=fixture['panes'][1]['pane_id']
 assert focused['pane_id']==expected,(product,focused,expected)
 proof[product]={'fold_rows':counts,'focused_pane_id':focused['pane_id'],'expected_pane_id':expected}
phases=[x.stem for x in (root/'fix1-keys/herdr').glob('*.txt') if x.stem=='middle' or x.stem.startswith(('normal-','search-'))]
selections={phase:[selected(p,phase) for p in ['herdr','gtx']] for phase in phases}
for phase,values in selections.items(): assert values[0]==values[1],(phase,values)
for phase in ['normal-n','normal-p','normal-shift-d','normal-alt-d','normal-shift-u','normal-alt-u']:
 assert selections[phase]==selections['middle'],(phase,selections)
for phase in ['search-home','search-end','search-shift-n','search-shift-p','search-alt-n','search-alt-p']:
 assert selections[phase]==selections['search-middle'],(phase,selections)
assert selections['search-next']!=selections['search-middle']
assert selections['search-prev']==selections['search-middle']
assert selections['normal-home']!=selections['normal-end']
assert selections['normal-up']!=selections['normal-end']
assert selections['normal-down']==selections['normal-end']
proof['key_selections']=selections
(root/'fix1-gate.json').write_text(json.dumps({'status':'PASS','proof':proof},ensure_ascii=False,indent=2)+'\n')
print('FIX-1 navigator screen/ID gate PASS')
