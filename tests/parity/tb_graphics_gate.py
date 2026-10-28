#!/usr/bin/env python3
"""Check T10/T17 against captured outer bytes and actual pane dimensions."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
p.add_argument('--cell-width', type=int, default=8)
p.add_argument('--cell-height', type=int, default=16)
p.add_argument('--reuse', action='store_true', help='check T7 with the static sibling image')
a = p.parse_args()
results = {}
for name in ('herdr', 'ikesu'):
    target = a.out / name
    runtime = json.loads((target / 'runtime-json.json').read_text())
    probe = json.loads(runtime['home/tb-probe.json'])
    queries = {r['query']: bytes.fromhex(r['response_hex']) for r in probe['queries']}
    expected = {14: f'\x1b[4;{probe["rows"]*a.cell_height};{probe["cols"]*a.cell_width}t'.encode(),
                16: f'\x1b[6;{a.cell_height};{a.cell_width}t'.encode(),
                18: f'\x1b[8;{probe["rows"]};{probe["cols"]}t'.encode()}
    query_pass = queries == expected
    raw = (target / 'terminal.raw').read_bytes()
    boundaries = re.findall(r'stats (\w+).*?outer_bytes_so_far=(\d+)', (target/'run.log').read_text())
    images, placements, stages, actions = {}, {}, {}, {}
    cursor = (0, 0)
    start = 0
    anonymous_placement = 0
    for stage, end in boundaries:
        data = raw[start:int(end)]
        actions[stage] = []
        for match in re.finditer(rb'\x1b\[(\d+);(\d+)H|\x1b_G(.*?)\x1b\\', data, re.S):
            if match[1] is not None:
                cursor = (int(match[2])-1, int(match[1])-1)
                continue
            header, _, payload = match[3].partition(b';')
            keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
            image_id = int(keys.get(b'i', b'0'))
            action = keys.get(b'a', b't')
            actions[stage].append(action.decode())
            if action == b't':
                if keys.get(b'm', b'0') != b'0' or image_id == 0:
                    raise SystemExit('fixture unexpectedly used chunked or anonymous upload')
                images[image_id] = base64.b64decode(payload, validate=True)
            elif action == b'p':
                placement_id = int(keys.get(b'p', b'0'))
                if placement_id == 0:
                    anonymous_placement -= 1
                    placement_id = anonymous_placement
                placements[(image_id, placement_id)] = (*cursor, int(keys[b'c']), int(keys[b'r']))
            elif action == b'd':
                if keys.get(b'd') == b'i':
                    placement_id = int(keys.get(b'p', b'0'))
                    placements = {k: v for k, v in placements.items()
                                  if not (k[0] == image_id and (placement_id == 0 or k[1] == placement_id))}
                    continue
                if keys.get(b'd') != b'I':
                    raise SystemExit('unhandled deletion in observation')
                images.pop(image_id, None)
                placements = {k: v for k, v in placements.items() if k[0] != image_id}
        stages[stage] = [{'geometry': list(v), 'pixels': images[k[0]].hex()} for k, v in placements.items()]
        start = int(end)
    placed, updated = stages['placed'], stages['updated']
    static = '00ff0000ff00'
    placed_dynamic = [r for r in placed if r['pixels'] != static]
    updated_dynamic = [r for r in updated if r['pixels'] != static]
    pixel_pass = (len(placed_dynamic) == len(updated_dynamic) == 1
                  and placed_dynamic[0]['pixels'] == 'ff000000ff00'
                  and updated_dynamic[0]['pixels'] == '0000ff00ff00'
                  and placed_dynamic[0]['geometry'] == updated_dynamic[0]['geometry'])
    results[name] = {'T10': pixel_pass, 'T17': query_pass, 'placements': stages,
                     'actions': actions,
                     'queries': probe['queries'], 'expected_queries': {k: v.hex() for k, v in expected.items()}}
    if a.reuse:
        changes = actions['updated']
        results[name]['T7'] = (len(placed) == len(updated) == 2
                               and [r for r in placed if r['pixels'] == static] == [r for r in updated if r['pixels'] == static]
                               and changes.count('t') == 1 and changes.count('d') == 1
                               and changes[-1] == 'd' and 'p' in changes[changes.index('t')+1:-1]
                               and actions['replaced'].count('t') == 0 and pixel_pass)
report = a.out / 'graphics-gate.json'
report.write_text(json.dumps(results, indent=2)+'\n')
checks = ('T7', 'T10', 'T17') if a.reuse else ('T10', 'T17')
print(json.dumps({name: {key: data[key] for key in checks} for name, data in results.items()}))
raise SystemExit(0 if all(r[k] for r in results.values() for k in checks) else 1)
