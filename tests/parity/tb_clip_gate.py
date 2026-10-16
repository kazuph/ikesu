#!/usr/bin/env python3
"""Check visible image area, source crop, and placement from real outer bytes."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
results = {}
for name in ('herdr', 'gtx'):
    root = a.out/name
    runtime = json.loads((root/'runtime-json.json').read_text())
    fixture = json.loads(runtime['home/tb-clip-input.json'])
    raw = (root/'terminal.raw').read_bytes()
    images, placements, stages = {}, {}, {}
    cursor, start = (0, 0), 0
    bounds = re.findall(r'stats (\S+).*?outer_bytes_so_far=(\d+)', (root/'run.log').read_text())
    for stage, end in bounds:
        for m in re.finditer(rb'\x1b\[(\d+);(\d+)H|\x1b_G(.*?)\x1b\\', raw[start:int(end)], re.S):
            if m[1] is not None:
                cursor = (int(m[2])-1, int(m[1])-1)
                continue
            header, _, payload = m[3].partition(b';')
            keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
            action = keys.get(b'a', b't')
            image_id, placement_id = int(keys.get(b'i', b'0')), int(keys.get(b'p', b'0'))
            if action == b't':
                if keys.get(b'm', b'0') != b'0' or int(keys.get(b'f', b'32')) != 24:
                    raise SystemExit('unsupported fixture upload')
                images[image_id] = (int(keys[b's']), int(keys[b'v']), base64.b64decode(payload, validate=True))
            elif action == b'p':
                placements[image_id, placement_id] = (cursor, keys)
            elif action == b'd':
                mode = keys.get(b'd', b'a')
                placements = {k: v for k, v in placements.items()
                              if not ((mode in (b'I', b'i') and k[0] == image_id and (not placement_id or k[1] == placement_id))
                                      or mode in (b'a', b'A'))}
        records = []
        for (image_id, _), (position, keys) in placements.items():
            width, height, data = images[image_id]
            x, y = int(keys.get(b'x', b'0')), int(keys.get(b'y', b'0'))
            w, h = int(keys.get(b'w', str(width).encode())), int(keys.get(b'h', str(height).encode()))
            pixels = b''.join(data[((y+r)*width+x)*3:((y+r)*width+x+w)*3] for r in range(h))
            records.append({'position': position, 'cells': (int(keys[b'c']), int(keys[b'r'])),
                            'source': (x, y, w, h), 'pixels': pixels.hex()})
        stages[stage] = records
        start = int(end)
    inside = stages['clip-inside']
    passed = len(inside) == 1 and inside[0]['pixels'] == fixture['pixels_hex']
    if passed:
        ox, oy = inside[0]['position'][0]-6, inside[0]['position'][1]-4
        for stage, position, cells, source in [
                ('clip-right', (ox+fixture['cols']-4, oy+4), (4, 4), (0, 0, 4, 4)),
                ('clip-bottom', (ox+6, oy+fixture['rows']-2), (8, 2), (0, 0, 8, 2))]:
            records = stages[stage]
            passed &= len(records) == 1 and records[0]['position'] == position and records[0]['cells'] == cells and records[0]['source'] == source
            if len(records) == 1:
                x, y, w, h = source
                full = bytes.fromhex(fixture['pixels_hex'])
                expected = b''.join(full[((y+r)*8+x)*3:((y+r)*8+x+w)*3] for r in range(h))
                passed &= records[0]['pixels'] == expected.hex()
    results[name] = {'passed': bool(passed), 'stages': stages}
(a.out/'clip-gate.json').write_text(json.dumps(results, indent=2)+'\n')
print(json.dumps({name: result['passed'] for name, result in results.items()}))
raise SystemExit(0 if all(result['passed'] for result in results.values()) else 1)
