#!/usr/bin/env python3
"""Interleave real resumed Claude displays, without sending conversation input."""
import argparse,hashlib,itertools,json,subprocess,sys
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['base','candidate','observer','source-session','out']:p.add_argument('--'+name,required=True)
p.add_argument('--names',nargs='+')
a=p.parse_args();base=Path(a.base).resolve();root=Path(a.out).resolve();root.mkdir(parents=True,exist_ok=True);harness=Path(__file__).parent
cases=[]
for i,((cols,rows),kind,rate) in enumerate(itertools.product([(106,38),(160,45),(250,70)],['vertical','horizontal','left','right'],[4,60,120])):
 cases.append(dict(name=f'{i:03d}-{cols}x{rows}-{kind}-claude-wait-{rate}hz',cols=cols,rows=rows,kind=kind,rate=rate,mode='claude-wait',panes=2,count=200))
if a.names:
 missing=set(a.names)-{c['name'] for c in cases}
 if missing:raise RuntimeError('unknown Claude conditions: '+str(sorted(missing)))
 cases=[c for c in cases if c['name'] in a.names]
(root/'conditions.json').write_text(json.dumps(cases,indent=2)+'\n')
for case in cases:
 for repeat in range(3):
  for product in ['ikesu','herdr']:
   binary=Path(a.candidate).resolve() if product=='ikesu' else Path.home()/'.local/share/herdr-builds/0.2.66/herdr'
   dest=root/f'{case["name"]}-r{repeat+1}-{product}'
   if (dest/'analysis.json').exists():
    old=json.loads((dest/'analysis.json').read_text());meta=old['metadata']
    if meta['binary_sha256']!=hashlib.sha256(binary.read_bytes()).hexdigest() or meta['observer_sha256']!=hashlib.sha256(Path(a.observer).read_bytes()).hexdigest() or meta['source_sha256']!=hashlib.sha256(Path(a.source_session).read_bytes()).hexdigest() or any(meta['scenario'][k]!=case[k] for k in ['cols','rows','kind','rate','mode','panes','count']):raise RuntimeError('Claude resume input mismatch')
    continue
   command=[sys.executable,str(harness/'claude_preflight.py'),'--base',str(base),'--binary',str(binary),'--observer',a.observer,'--source-session',a.source_session,'--product',product,'--out',str(dest),'--cols',str(case['cols']),'--rows',str(case['rows']),'--kind',case['kind'],'--rate',str(case['rate']),'--count',str(case['count'])]
   subprocess.run(command,check=True)
   observed=dest if (dest/'metadata.json').exists() else Path(str(dest)+'-temporary')
   cleanup=json.loads((observed/'cleanup.json').read_text())
   if not all(cleanup[k] for k in ['claude_normal_exit','original_session_unchanged','copied_sessions_removed','temporary_projects_removed','temporary_workdirs_removed']) or cleanup['server_exit']!=0:raise RuntimeError('Claude cleanup was not verified')
   subprocess.run([sys.executable,str(harness/'correlate_claude_wait.py'),str(observed)],check=True)
   subprocess.run([sys.executable,str(harness/'analyze_v2.py'),str(observed),'--timeline','timeline-correlated.jsonl','--app-glob','correlated-app-*.jsonl'],check=True,stdout=subprocess.DEVNULL)
   analysis=json.loads((observed/'analysis.json').read_text());analysis['metadata']['observation_directory']=str(observed)
   (dest/'analysis.json').write_text(json.dumps(analysis,indent=2)+'\n')
   with (root/'completed.jsonl').open('a') as log:log.write(json.dumps(dict(condition=case['name'],repeat=repeat+1,product=product,analysis=str(dest/'analysis.json'),observation_directory=str(observed)))+'\n')
