#!/usr/bin/env python3
"""Reconstruct delivery from timestamped outer-terminal image records only."""
import argparse, collections, json, math, pathlib, statistics
p=argparse.ArgumentParser();p.add_argument('directory');a=p.parse_args();root=pathlib.Path(a.directory)
report={}
for mode in ('direct','nested'):
 meta=json.loads((root/f'{mode}-metadata.json').read_text())
 events=[json.loads(line) for line in (root/f'{mode}.jsonl').read_text().splitlines()]
 decoded={e['id']:e for e in events if e['type']=='image.decoded'}
 click=next(e for e in events if e['type']=='mouse.input' and e['kind']=='down')['clock_ns']
 placed=[e for e in events if e['type']=='image.placed' and 'image_center_px' in e]
 # Pond fish use a square rotation box and a multi-color palette. The row fish is 16px high.
 fish=collections.defaultdict(list);waves=[]
 for e in placed:
  img=decoded[e['id']];box=e['display_px'];colors=img['opaque_colors']
  if len(colors)==1:
   if e['clock_ns']>=click:waves.append(e)
  elif box[3]>=40:
   # Source placement IDs remain stable while the sprite image ID changes with its tail/direction.
   fish[e['control'].get('p','1')].append(e)
 results=[];cx,cy=meta['click_px']
 for key,points in sorted(fish.items()):
  points.sort(key=lambda e:e['clock_ns'])
  free=[e for e in points if click-9_000_000_000<=e['clock_ns']<click-500_000_000]
  if not free:continue
  duration=8.5
  def center(e):return e['image_center_px']
  # Source 40-dot square is enlarged by an integer factor; use the received scale.
  scale=decoded[free[0]['id']]['width']/40*free[0]['display_px'][2]/free[0]['source_px'][2]
  distance=sum(math.dist(center(x),center(y))/scale for x,y in zip(free,free[1:]))
  start=max((e for e in points if e['clock_ns']<=click),key=lambda e:e['clock_ns'])
  end=max((e for e in points if e['clock_ns']<=click+1_500_000_000),key=lambda e:e['clock_ns'])
  d0=math.dist(center(start),(cx,cy))/scale;d1=math.dist(center(end),(cx,cy))/scale
  gaps=[(y['clock_ns']-x['clock_ns'])/1e6 for x,y in zip(free,free[1:])]
  results.append({'placement':key,'updates_per_second':len(free)/duration,'source_dots_per_second':distance/duration,'max_delivery_gap_ms':max(gaps,default=None),'distance_at_click_dots':d0,'distance_at_1_5s_dots':d1,'distance_ratio':d1/d0 if d0 else 0,'last_update_at_1_5s_ms':(end['clock_ns']-click)/1e6,'scale':scale})
 replies=[e for e in events if e['type']=='terminal.status_complete']
 wait=sum((e.get('replied_ns',e['clock_ns'])-e['clock_ns'])/1e6 for e in replies)
 report[mode]={'metadata':meta,'fish':results,'ripple_frames':[{'after_click_ms':(e['clock_ns']-click)/1e6,'radius_px':max((math.dist(point,meta['click_px']) for point in e.get('visible_alpha_points_px',[])),default=e.get('visible_radius_px',0)),'visible_alpha_box_px':e.get('visible_alpha_box_px'),'display_px':e['display_px']} for e in waves], 'terminal_status_queries':len(replies),'outer_status_reply_wait_total_ms':wait,'ripple_first_ms':(waves[0]['clock_ns']-click)/1e6 if waves else None,'ripple_max_radius_px':max((math.dist(point,meta['click_px']) for e in waves for point in e.get('visible_alpha_points_px',[])),default=max((e.get('visible_radius_px',0) for e in waves),default=0))}
(root/'wire-report.json').write_text(json.dumps(report,indent=2));print(json.dumps(report,indent=2))
