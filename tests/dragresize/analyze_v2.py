#!/usr/bin/env python3
"""Correlate separate input, border, PTY receipt, and application frame clocks."""
import argparse,json,math,statistics
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('directory');a=p.parse_args();d=Path(a.directory)
def load(path):return [json.loads(l) for l in path.read_text().splitlines()] if path.exists() else []
def quantile(xs,p):return sorted(xs)[max(0,math.ceil(p*len(xs))-1)] if xs else None
def summary(xs):return dict(n=len(xs),median=statistics.median(xs) if xs else None,p95=quantile(xs,.95),maximum=max(xs) if xs else None)
events=load(d/'timeline.jsonl');meta=json.loads((d/'metadata.json').read_text());trial=next(e for e in events if e['type']=='trial');inputs=[e for e in events if e['type']=='input'];release=next(e for e in events if e['type']=='release');finish=next(e for e in events if e['type']=='finish');frames=[e for e in events if e['type']=='frame'];inputs.sort(key=lambda e:e['clock_ns']);frames.sort(key=lambda e:e['clock_ns']);
for f in frames:f['corners']=f['corners'] or [];f['panes']=f['panes'] or []
cfg=trial['config'];wanted={'vertical':'┐','horizontal':'└','left':'┌','right':'┐'}[cfg['kind']]
def matches(f,e):return any(c['X']==e['watch_x'] and c['Y']==e['watch_y'] and c['Glyph']==wanted for c in f['corners'])
applications={}
for f in d.glob('app-*.jsonl'):
 for e in load(f):
  if e['event'] in ['initial','pty_size','app_written']:applications[(e['id'],e['seq'],e['event'])]=e
motion=[];miss=[];app_motion=[];app_miss=[]
for i,e in enumerate(inputs):
 end=inputs[i+1]['clock_ns'] if i+1<len(inputs) else release['clock_ns']
 observed=next((f for f in frames if e['clock_ns']<=f['clock_ns']<=end and matches(f,e)),None)
 if observed:motion.append(dict(index=i,input_ns=e['clock_ns'],border_ns=observed['clock_ns'],latency_ms=(observed['clock_ns']-e['clock_ns'])/1e6))
 else:miss.append(i)
 correct=next((f for f in frames if e['clock_ns']<=f['clock_ns']<=end and matches(f,e) and len(f['panes'])==meta['scenario'].get('panes',2) and all(v['Width']==v.get('LayoutWidth') and v['Height']==v.get('LayoutHeight') for v in f['panes'])),None)
 if correct:
  deliveries=[]
  for v in correct['panes']:
   receipt=applications.get((v['ID'],v['Seq'],'pty_size'))
   if receipt and receipt['clock_ns']>=e['clock_ns']:deliveries.append(dict(id=v['ID'],clock_ns=receipt['clock_ns'],latency_ms=(receipt['clock_ns']-e['clock_ns'])/1e6))
  app_motion.append(dict(index=i,input_ns=e['clock_ns'],app_visible_ns=correct['clock_ns'],latency_ms=(correct['clock_ns']-e['clock_ns'])/1e6,pty_deliveries=deliveries))
 else:app_miss.append(i)
# Keep only meaningful screen updates; protocol-only/duplicate writes are not
# visible progress and must not make an update interval look shorter.
visible=[];prior=None
for f in frames:
 if not inputs[0]['clock_ns']<=f['clock_ns']<=finish['clock_ns']:continue
 key=(tuple((c['X'],c['Y'],c['Glyph']) for c in f['corners']),tuple((v['ID'],v['Seq'],v['Width'],v['Height']) for v in f['panes']))
 if key!=prior:visible.append(f);prior=key
def ready(f):return matches(f,release) and len(f['panes'])==meta['scenario'].get('panes',2) and all(v['Width']==v.get('LayoutWidth') and v['Height']==v.get('LayoutHeight') for v in f['panes'])
before=[f for f in frames if f['clock_ns']<=release['clock_ns']]
already=bool(before and ready(before[-1]))
final=before[-1] if already else next((f for f in visible if f['clock_ns']>=release['clock_ns'] and ready(f)),None)
# G3 includes the trailing interval until final geometry and application
# content settle. If they never settle, the entire observed stalled tail stays.
end_ns=max(release['clock_ns'],final['clock_ns']) if final else finish['clock_ns']
visible=[f for f in visible if f['clock_ns']<=end_ns]
lag=[];boundary_lag=[];correlation=[];last_good=inputs[0]['clock_ns']
for f in visible:
 eligible=[e for e in inputs+[release] if e['clock_ns']<=f['clock_ns'] and matches(f,e)]
 if not eligible:continue
 e=max(eligible,key=lambda e:e['clock_ns']);boundary_lag.append((f['clock_ns']-e['clock_ns'])/1e6)
 if len(f['panes'])==meta['scenario'].get('panes',2) and all(v['Width']==v.get('LayoutWidth') and v['Height']==v.get('LayoutHeight') for v in f['panes']):last_good=e['clock_ns']
 lag.append((f['clock_ns']-last_good)/1e6)
 for v in f['panes']:
  receipt=applications.get((v['ID'],v['Seq'],'pty_size'));written=applications.get((v['ID'],v['Seq'],'app_written'))
  if not written:continue
  correlation.append(dict(input_ns=e['clock_ns'],border_ns=f['clock_ns'],id=v['ID'],seq=v['Seq'],pty_receipt_ns=None if receipt is None else receipt['clock_ns'],app_written_ns=written['clock_ns'],app_visible_ns=f['clock_ns'],app_dimensions=[v['Width'],v['Height']],layout_dimensions=[v.get('LayoutWidth'),v.get('LayoutHeight')]))
# Include the initial gap and an unfinished trailing gap. A stalled tail cannot
# disappear merely because no frame was observed there.
points=[inputs[0]['clock_ns']]+[f['clock_ns'] for f in visible]+[end_ns]
intervals=[(b-a)/1e6 for a,b in zip(points,points[1:]) if b>a]
blocks=[];app_blocks=[]
for start in range(0,cfg['count'],10):
 values=[e['latency_ms'] for e in motion if start<=e['index']<start+10];blocks.append(dict(start=start,observed=len(values),median_ms=statistics.median(values) if len(values)==10 else None))
 values=[e['latency_ms'] for e in app_motion if start<=e['index']<start+10];app_blocks.append(dict(start=start,observed=len(values),median_ms=statistics.median(values) if len(values)==10 else None))
resource={}
for role in ['server','client']:
 stats=load(d/f'{role}-runtime.jsonl');stats=[s for s in stats if inputs[0]['clock_ns']<=s['clock_ns']<=end_ns]
 if not stats:continue
 first,last=stats[0],stats[-1];pauses=last['/gc/pauses:seconds'];before=first['/gc/pauses:seconds'];counts=[b-a for a,b in zip(before['counts'],pauses['counts'])];bounds=pauses['buckets'];maximum=max((i for i,n in enumerate(counts) if n),default=None)
 resource[role]=dict(sampling_first_ns=first['clock_ns'],sampling_last_ns=last['clock_ns'],allocated_bytes=last['/gc/heap/allocs:bytes']-first['/gc/heap/allocs:bytes'],heap_live_bytes=last['/gc/heap/live:bytes'],heap_objects_bytes=last['/memory/classes/heap/objects:bytes'],gc_pause_count=sum(counts),gc_pause_max_seconds_bucket=None if maximum is None else [bounds[maximum],bounds[maximum+1]],panes=last['panes'])
result=dict(metadata=meta,input_count=len(inputs),missing_slow_observations=miss,slow=summary([e['latency_ms'] for e in motion]),application=summary([e['latency_ms'] for e in app_motion]),pty_delivery=summary([v['latency_ms'] for e in app_motion for v in e['pty_deliveries']]),missing_application_observations=app_miss,app_motion=app_motion,boundary_lag_ms=summary(boundary_lag),blocks=blocks,app_blocks=app_blocks,update_interval_ms=summary(intervals),tracking_lag_ms=summary(lag),release_settle_ms=None if final is None else max(0,final['clock_ns']-release['clock_ns'])/1e6,final_dimensions_verified=final is not None,resource=resource,correlation=correlation,motion=motion)
(d/'analysis.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k not in ['metadata','correlation','motion','app_motion','missing_slow_observations','missing_application_observations','blocks','app_blocks']}))
