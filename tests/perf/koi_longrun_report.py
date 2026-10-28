#!/usr/bin/env python3
"""Audit the received terminal's image inventory and minute-by-minute RSS/wire."""
import argparse,json,pathlib,re
p=argparse.ArgumentParser();p.add_argument('directory');p.add_argument('--commit',required=True);a=p.parse_args();root=pathlib.Path(a.directory)
results=json.loads((root/'results.json').read_text());reports=[]
for result in results:
 live={};live_bytes=peak_count=peak_bytes=uploads=deletes=waves=0
 inventories=[];first_clock=None;next_minute=1
 timeline=pathlib.Path(result['raw_output']).with_suffix('.timeline.jsonl')
 with timeline.open() as source:
  for line in source:
   event=json.loads(line);kind=event.get('type')
   clock=event.get('clock_ns')
   if clock is not None:
    if first_clock is None:first_clock=clock
    if clock-first_clock>=next_minute*60*1e9:
     inventories.append({'minute':next_minute,'images':len(live),'rgba_bytes':live_bytes,'uploads':uploads,'deletes':deletes});next_minute+=1
   if kind=='image.decoded':
    live_bytes+=event['rgba_bytes']-live.get(event['id'],0);live[event['id']]=event['rgba_bytes'];uploads+=1
   elif kind=='image.deleted':
    control=event['control']
    if control.get('d')=='A':live.clear();live_bytes=0
    elif control.get('d')=='I':live_bytes-=live.pop(int(control['i']),0);deletes+=1
   elif kind=='image.placed' and event['control'].get('z')=='400':waves+=1
   # A frame can upload before retiring its predecessor; record that transient
   # separately. The post-retirement inventory is asserted by the Go wire test.
   peak_count=max(peak_count,len(live));peak_bytes=max(peak_bytes,live_bytes)
 samples=[]
 for label,pid,rss,cpu,wire in re.findall(r'stats (before|minute-\d+|after) pid=(\d+) rss_kb,cpu=(\d+)\s+([0-9:.]+) outer_bytes_so_far=(\d+)',pathlib.Path(result['log']).read_text()):
  samples.append({'label':label,'pid':int(pid),'rss_kb':int(rss),'cpu':cpu,'terminal_bytes':int(wire)})
 assert result['seconds']>=1800,'30-minute run required'
 assert len(samples)>=32,'missing minute samples'
 assert waves>0,'no click-generated wave reached the terminal'
 assert deletes>0,'stress did not reach terminal retirement'
 assert len(live)==0,'client shutdown left terminal images'
 for before,after in zip(samples,samples[1:]):after['bytes_since_previous']=after['terminal_bytes']-before['terminal_bytes']
 report={'commit':a.commit,'terminal_inventory':inventories,'seconds':result['seconds'],'uploads':uploads,'image_deletions':deletes,'wave_placements':waves,'peak_terminal_images_including_inflight_frame':peak_count,'peak_terminal_rgba_bytes_including_inflight_frame':peak_bytes,'terminal_images_after_exit':len(live),'samples':samples,'rss_min_kb':min(x['rss_kb'] for x in samples),'rss_max_kb':max(x['rss_kb'] for x in samples)}
 reports.append(report)
(root/'longrun-proof.json').write_text(json.dumps(reports,indent=2));print(json.dumps(reports,indent=2))
