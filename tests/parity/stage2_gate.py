#!/usr/bin/env python3
"""Compare only stage-two claims; save raw facts and fail on missing evidence."""
import argparse
import json
from pathlib import Path
import re
import sys
parser=argparse.ArgumentParser();parser.add_argument('root');parser.add_argument('--only',nargs='*');args=parser.parse_args()
root=Path(args.root);results=[]
def screen(case,prod,name):
 p=root/case/prod/(name+'.txt')
 if not p.is_file(): raise AssertionError('missing '+str(p))
 return p.read_text()
def check(case,name,extract,expected=None):
 if args.only and case not in args.only:return
 try:
  values={prod:extract(prod) for prod in ['herdr','gtx']}
  passed=values['herdr']==values['gtx'] and (expected is None or values['herdr']==expected)
  results.append(dict(case=case,claim=name,values=values,expected=expected,result='PASS' if passed else 'FAIL'))
 except Exception as e:results.append(dict(case=case,claim=name,result='FAIL',error=str(e)))
def has(case,frame,word):return lambda prod:word in screen(case,prod,frame)
def selected(case,frame):
 def extract(prod):
  data=json.loads((root/case/prod/(frame+'.json')).read_text())
  lines=[''.join(c['Content'] for c in row[:26]) for row in data['Cells']]
  return [re.sub(r'^▌[· ]*\d+\s*','',line).strip() for line in lines if line.startswith('▌') and re.search(r'\d+',line)]
 return extract

def focused(case,prod,frame):
 data=json.loads((root/case/prod/(frame+'.json')).read_text())
 row=data['Cells'][1]
 ids=[int(re.match(r'%([0-9]+)', ''.join(c['Content'] for c in row[i:])).group(1)) for i,cell in enumerate(row) if cell['Content']=='%' and cell['Style']['Attrs'] & 1]
 if len(ids)!=1:raise AssertionError('expected exactly one bold focused pane title: '+str(ids))
 return ids[0]

for c in ['workspace-rename','workspace-close','workspace-worktree']:
 check(c,'selected beta before action',lambda p:'beta' in selected(c,'selected')(p),True)
 if c=='workspace-rename': check(c,'rename uses displayed workspace',lambda p:bool(re.search(r'│ parity +│',screen(c,p,'after'))),True)
 if c=='workspace-close': check(c,'close confirmation uses displayed workspace',has(c,'after','parity — 1 pane'),True)
 if c=='workspace-worktree':check(c,'worktree action executes and reports non-repository',has(c,'after','worktree actions require a workspace inside a Git work tree'),True)
check('workspace-number','numeric selection uses visible order',selected('workspace-number','after'),['gamma'])
for case in ['workspace-cancel-rename','workspace-cancel-close']:
 check(case,'Esc closes dialog',lambda p:('rename workspace' in screen(case,p,'cancelled') or 'Close workspace?' in screen(case,p,'cancelled')),False)
 check(case,'Esc navigate state',has(case,'cancelled','NAVIGATE'))
 check(case,'Esc workspace selection',selected(case,'cancelled'))
for mode in ['enter','r','direct']:
 case='resize-'+mode
 check(case,'resize entered',has(case,'before','RESIZE'),True)
 check(case,'resize exited',has(case,'after','RESIZE'),False)
 check(case,'subsequent text reaches shell',has(case,'after','stage2_marker'),True)
case='navigate-custom'
check(case,'custom command leaves navigate',has(case,'navigate','NAVIGATE'),False)
check(case,'custom command opens one additional pane',lambda p:int(re.search(r'%([0-9]+)',screen(case,p,'navigate')).group(1))-int(re.search(r'%([0-9]+)',screen(case,p,'normal')).group(1)),1)
for mode,stay in [('tab',True),('direction',True),('next',False),('zoom',False)]:
 case='exit-detail-'+mode
 check(case,'action navigate state',has(case,'action','NAVIGATE'),stay)
 check(case,'unbound letter navigate state',has(case,'letter-u','NAVIGATE'),stay)
 check(case,'sidebar action exits navigate',has(case,'letter-b','NAVIGATE'),False)
 check(case,'subsequent text reaches shell',has(case,'after','marker'),True)
for case in ['navigate-split','navigate-next-tab','navigate-zoom']:
 check(case,'action exits navigate',has(case,'action','NAVIGATE'),False)
 check(case,'ordinary input reaches pane',has(case,'after','abcdef'),True)
for name in ['backslash','bs','colon','command','hyper','meta','period','quote','return','semicolon','slash']:
 case='alias-'+name
 check(case,'alias opens help',has(case,'after','keybinds'),True)
for frame in ['kitty','plain']:check('shift-slash',frame+' opens help',has('shift-slash',frame,'keybinds'),True)
for i in range(1,10):
 for form in ['symbol','base']:
  check('indexed-workspace',f'{form} {i} selects workspace',selected('indexed-workspace',f'{form}-{i}'),['parity' if i==1 else f'indexed-{i}'])
for i in range(1,10):
 other=9 if i==1 else i-1
 check('user-all-numbers',f'cmd+{i} starts on a different expected workspace',selected('user-all-numbers',f'before-number-{i}'),['parity' if other==1 else f'indexed-{other}'])
 check('user-all-numbers',f'unchanged cmd+{i} selects expected workspace',selected('user-all-numbers',f'number-{i}'),['parity' if i==1 else f'indexed-{i}'])
 check('user-all-numbers',f'cmd+{i} changes selection',lambda p:selected('user-all-numbers',f'before-number-{i}')(p)!=selected('user-all-numbers',f'number-{i}')(p),True)
for case in ['config-invalid','config-printable']:
 check(case,'invalid binding does not restore default',has(case,'default','keybinds'),False)
 check(case,'plain a reaches shell',lambda p:bool(re.search(r'PARITY> a\s',screen(case,p,'plain'))),True)
for case in ['config-conflict','config-reverse-conflict','config-array']:
 check(case,'accepted direct binding opens help',has(case,'direct','keybinds'),True)
check('config-default-conflict','explicit binding replaces default owner',has('config-default-conflict','default','keybinds'),True)
for case in ['config-invalid','config-printable','config-conflict','config-reverse-conflict','config-array']:
 def diagnostic(prod):
  folder=root/case/prod
  content='\n'.join(p.read_text(errors='replace') for name in ['runtime-logs.json','server.log','startup.txt'] if (p:=folder/name).is_file())
  return ('invalid keybinding' in content or 'unsafe direct keybinding' in content or 'unmodified printable' in content or 'disabled keys.settings' in content or 'conflicts with keys.help' in content)
 check(case,'diagnostic recorded',diagnostic,True)
for frame in ['one','three','next','previous','history-back','history-forward']:
 check('user-direct',frame+' selection matches',selected('user-direct',frame))
check('navigate-direction-remapped','navigate remains active',has('navigate-direction-remapped','action','NAVIGATE'),True)
check('navigate-direction-remapped','independent direction moves focus',lambda p:focused('navigate-direction-remapped',p,'action')-focused('navigate-direction-remapped',p,'split'),-1)
for scope in ('prefix','direct'):
 for suffix in ('','-reverse'):
  case='review-conflict-'+scope+suffix
  for frame in ('plain','kitty'):
   check(case,frame+' help wins',has(case,frame,'keybinds'),True)
   check(case,frame+' both configured punctuation bindings remain visible',lambda p: any('?' in line and 'keybinds' in line for line in screen(case,p,frame).splitlines()) and any('shift+' in line and '?' in line and 'settings' in line for line in screen(case,p,frame).splitlines()),True)
  def no_conflict(prod):
   folder=root/case/prod
   content='\n'.join(p.read_text(errors='replace') for name in ['runtime-logs.json','server.log','startup.txt'] if (p:=folder/name).is_file())
   return not any(word in content.lower() for word in ['conflicts with','disabled keys.settings','disabling binding'])
  check(case,'no conflict diagnostic, matching real 0.2.57',no_conflict,True)
for key in ('slash','digit'):
 for binding in ('base','symbol'):
  case='review-'+key+'-'+binding
  for frame in ('base','alternate','symbol'):
   check(case,frame+' help activation',has(case,frame,'keybinds'),frame=='alternate' or frame==binding)
# Every run must show real completion, a versioned reference and isolated cleanup.
for case in sorted({r['case'] for r in results}):
 check(case,'exact reference version',lambda p:json.loads((root/case/'reference.json').read_text())['herdr_version'],'herdr 0.2.57')
 for prod in ['herdr','gtx']:
  folder=root/case/prod
  try:
   if 'TIMEOUT' in (folder/'run.log').read_text():raise AssertionError('TIMEOUT')
   cleanup=json.loads((folder/'cleanup.json').read_text())
   results.append(dict(case=case,claim=prod+' cleanup',values=cleanup,result='PASS' if cleanup.get('exit')==0 and len(cleanup.get('replies',[]))==1 and json.loads(cleanup['replies'][0]).get('result',{}).get('type')=='ok' else 'FAIL'))
  except Exception as e:results.append(dict(case=case,claim=prod+' cleanup',result='FAIL',error=str(e)))
report=dict(results=results,passed=sum(x['result']=='PASS' for x in results),failed=sum(x['result']!='PASS' for x in results))
(root/'stage2-gate.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
for r in results:
 if r['result']!='PASS':print(json.dumps(r,ensure_ascii=False))
print(f"stage2 claims: {report['passed']} PASS, {report['failed']} FAIL")
raise SystemExit(bool(report['failed']))
