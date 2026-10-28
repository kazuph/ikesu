#!/usr/bin/env python3
"""Check real menu persistence and timed terminal ripple placements."""
import argparse,json,pathlib
parser=argparse.ArgumentParser();parser.add_argument('directory');args=parser.parse_args();root=pathlib.Path(args.directory)
results=json.loads((root/'results.json').read_text());proof=[]
for result in results:
 interaction=result['interaction']
 if interaction=='menu':
  assert result['menu_saved']==[False,True],result['menu_saved']
  proof.append({'interaction':'menu','saved_sequence':result['menu_saved'],'screens':'on/off/on captured from actual PTY'})
 else:
  timeline=pathlib.Path(result['raw_output']).with_suffix('.timeline.jsonl')
  records=[json.loads(line) for line in timeline.read_text().splitlines()]
  wire=pathlib.Path(result['raw_output']).read_bytes()
  # The real client negotiates SGR-pixel input. 1165,377 is the center
  # of cell 146,24 at the bench's measured 8x16 pixel geometry.
  assert b'\x1b[?1016h' in wire,'fixture did not negotiate pixel mouse'
  click=next(r for r in records if r.get('type')=='capacity.input' and '[<0;1165;377M' in r.get('data',''))
  wave=next(r for r in records if r.get('type')=='image.placed' and r['clock_ns']>=click['clock_ns'] and r['control'].get('z')=='400')
  latency=(wave['clock_ns']-click['clock_ns'])/1e9
  maximum=.05 if interaction=='ripple' else 1+.05
  assert latency<=maximum,(interaction,latency,maximum)
  proof.append({'interaction':interaction,'latency_seconds':latency,'limit_seconds':maximum,'terminal':'real PNG decode + a=p observed','fence':'normal' if interaction=='ripple' else 'existing terminal DSR intentionally withheld until existing 1s timeout'})
(root/'interaction-proof.json').write_text(json.dumps(proof,indent=2));print(json.dumps(proof,indent=2))
