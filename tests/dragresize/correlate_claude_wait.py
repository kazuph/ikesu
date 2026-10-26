#!/usr/bin/env python3
"""Match unmodified Claude stdout frames to actual outer pane grids exactly."""
import argparse,json
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('directory');a=p.parse_args();d=Path(a.directory)
def load(path):return [json.loads(line) for line in path.read_text().splitlines()]
events=load(d/'timeline.jsonl');meta=json.loads((d/'metadata.json').read_text());inner={};receipts={};numbered={};correlation=[]
for id in [1,2]:
 values=load(d/f'claude-{id}.jsonl');inner[id]=[];receipts[id]={}
 for e in values:
  if e['type']=='claude.pty_size':receipts[id][e['size_sequence']]=e
  if e['type']=='claude.frame':
   # This is the observed stdout snapshot number, not an invented
   # application-internal sequence or a marker inserted into Claude's screen.
   e=dict(e,observed_frame=len(inner[id])+1);inner[id].append(e)
 numbered[id]=[]
def roots(bodies):
 result=[]
 for b in bodies:
  if not any(other is not b and other['X']<=b['X'] and other['Y']<=b['Y'] and other['X']+other['Width']>=b['X']+b['Width'] and other['Y']+other['Height']>=b['Y']+b['Height'] and other['Width']*other['Height']>b['Width']*b['Height'] for other in bodies):result.append(b)
 return sorted(result,key=lambda b:(b['Y'],b['X'])) if meta['trial']['kind']=='horizontal' else sorted(result,key=lambda b:(b['X'],b['Y']))
output=[];pending=None;seen_frames=0;matched_frames=0
for e in events:
 if e['type']=='frame':pending=dict(e,panes=[]);continue
 if e['type']!='bodies':output.append(e);continue
 if not e['synchronized'] or pending is None:continue
 seen_frames+=1;at=e['clock_ns'];bodies=roots(e['bodies'] or [])
 if len(bodies)==2:
  for id,body in enumerate(bodies,1):
   prior=[v for v in inner[id] if v['clock_ns']<=at and v['changed_since_resize']]
   latest_size=max((r for r in receipts[id].values() if r['clock_ns']<=at),key=lambda r:r['clock_ns'],default=None)
   size_sequence=latest_size['size_sequence'] if latest_size else 0
   match=next((v for v in reversed(prior) if v['size_sequence']==size_sequence and v['width']==body['Width'] and v['height']==body['Height'] and v['text_hash']==body['Hash']),None)
   if match is None:continue
   pending['panes'].append(dict(ID=id,Seq=match['observed_frame'],Width=match['width'],Height=match['height'],LayoutWidth=body['Width'],LayoutHeight=body['Height'],X=body['X'],Y=body['Y'],provenance='exact-unmodified-stdout-snapshot-hash',source_synchronized=match['synchronized']))
   if latest_size:
    numbered[id].append(dict(event='pty_size',clock_ns=latest_size['clock_ns'],id=id,seq=match['observed_frame'],width=match['width'],height=match['height']))
   correlation.append(dict(id=id,observed_stdout_frame=match['observed_frame'],pty_size_sequence=size_sequence,pty_size_ns=None if latest_size is None else latest_size['clock_ns'],app_source_seen_ns=match['clock_ns'],app_visible_ns=at,width=body['Width'],height=body['Height'],text_hash=body['Hash'],source_synchronized=match['synchronized']))
 if len(pending['panes'])==2:matched_frames+=1
 output.append(pending);pending=None
(d/'timeline-correlated.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in output))
for id,items in numbered.items():(d/f'correlated-app-{id}.jsonl').write_text(''.join(json.dumps(e)+'\n' for e in items))
(d/'claude-correlation.json').write_text(json.dumps(dict(complete_outer_frames=seen_frames,exactly_matched_frames=matched_frames,observations=correlation),indent=2)+'\n')
print(json.dumps(dict(complete_outer_frames=seen_frames,exactly_matched_frames=matched_frames,observations=len(correlation))))
