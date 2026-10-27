#!/usr/bin/env python3
"""Audit the real ptyrun wire: demand uploads, retained IDs and PNG byte counts."""
import argparse,base64,json,pathlib,re,statistics
p=argparse.ArgumentParser();p.add_argument('directory');args=p.parse_args()
root=pathlib.Path(args.directory)
results=json.loads((root/'results.json').read_text())
proof=[]
for result in results:
 wire=pathlib.Path(result['raw_output']).read_bytes()
 uploads={};image_bytes=0;upload_wire_bytes=0;first_upload=None;initial_end=None;initial_bytes=0;packets=0
 for match in re.finditer(rb'\x1b_G([^;]*);(.*?)\x1b\\',wire,re.S):
  header,payload=match.groups();fields=dict(item.split(b'=',1) for item in header.split(b',') if b'=' in item)
  if fields.get(b'a')==b't':
   image_id=int(fields[b'i']);assert image_id not in uploads,f'image ID {image_id} uploaded twice'
   uploads[image_id]={'png_bytes':0,'wire_bytes':0};current=image_id
   if first_upload is None:
    first_upload=match.start();initial_end=wire.find(b'\x1b[?2026l',match.end())
  elif b'm' not in fields or b'a' in fields:continue
  decoded=base64.b64decode(payload);image_bytes+=len(decoded);uploads[current]['png_bytes']+=len(decoded)
  count=match.end()-match.start();upload_wire_bytes+=count;uploads[current]['wire_bytes']+=count;packets+=1
  if match.start()<initial_end:initial_bytes+=count
 proof.append({'session':result['session'],'enabled':result['enabled'],'condition':result['condition'],'state':result.get('state'),'seconds':result['seconds'],'images':len(uploads),'first_image_frame_upload_bytes':initial_bytes,'total_png_bytes':image_bytes,'total_upload_wire_bytes':upload_wire_bytes,'upload_packets':packets,'duplicates':0,'max_mean_upload_bytes_per_second':upload_wire_bytes/result['seconds'],'terminal_bytes_during_window':result['terminal_bytes']})
summary=[]
for condition in sorted({r['condition'] for r in results}):
 group=[r for r in results if r['condition']==condition]
 def median(field,on):return statistics.median(r[field] for r in group if r['enabled']==on)
 off,on=median('cpu_seconds',False),median('cpu_seconds',True)
 seconds=group[0]['seconds']
 delta=(on-off)/seconds*100
 for r in group:
  if r['condition']!='empty':assert r['server_bytes']==0 and r['snapshot_messages']==0,'decoration caused server transport'
 summary.append({'condition':condition,'seconds':seconds,'off_cpu_seconds':off,'on_cpu_seconds':on,'additional_one_core_percent':delta,'off_terminal_bytes':median('terminal_bytes',False),'on_terminal_bytes':median('terminal_bytes',True),'server_transport':'0 bytes / 0 snapshots' if condition!='empty' else 'standalone','under_one_percent':delta<1})
output={'wire':proof,'summary':summary}
(root/'protocol.json').write_text(json.dumps(output,indent=2));print(json.dumps(output,indent=2))
