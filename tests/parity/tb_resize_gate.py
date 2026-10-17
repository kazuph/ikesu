#!/usr/bin/env python3
"""Count resize clears and verify retained image pixels/geometry from real PTYs."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
results = {}
for product in ('herdr', 'gtx'):
    root = a.out/product
    raw = (root/'terminal.raw').read_bytes()
    log = (root/'run.log').read_text()
    marks = dict(re.findall(r'event mark label="(resize-start|resize-end)" at_unix_ms=\d+ outer_bytes=(\d+)', log))
    begin, end = int(marks['resize-start']), int(marks['resize-end'])
    assert 0 <= begin < end <= len(raw)
    period = raw[begin:end]
    images, placements, stages = {}, {}, []
    cursor = (0, 0)
    start = 0
    for stop in (begin, end):
        for match in re.finditer(rb'\x1b\[(\d+);(\d+)H|\x1b_G(.*?)\x1b\\', raw[start:stop], re.S):
            if match[1] is not None:
                cursor = (int(match[2])-1, int(match[1])-1)
                continue
            header, _, payload = match[3].partition(b';')
            keys = dict(f.split(b'=', 1) for f in header.split(b',') if b'=' in f)
            action, image = keys.get(b'a', b't'), int(keys.get(b'i', b'0'))
            if action == b't':
                assert keys.get(b't', b'd') == b'd' and keys.get(b'm', b'0') == b'0'
                images[image] = base64.b64decode(payload, validate=True).hex()
            elif action == b'p':
                placements[(image, int(keys.get(b'p', b'0')))] = [*cursor, int(keys[b'c']), int(keys[b'r'])]
            elif action == b'd':
                placements = {key: value for key, value in placements.items() if key[0] != image}
        stages.append([{'geometry': geometry, 'pixels': images[key[0]]} for key, geometry in placements.items()])
        start = stop
    counts = {'screen_clears': period.count(b'\x1b[2J'), 'uploads': period.count(b'a=t,'), 'deletes': period.count(b'a=d,')}
    passed = (len(stages[0]) == 1 and stages[0] == stages[1]
              and stages[0][0]['pixels'] == 'ff000000ff00' and not any(counts.values()))
    results[product] = {'passed': passed, 'counts': counts, 'stages': stages}
equal = results['herdr']['stages'] == results['gtx']['stages']
report = {'scope': 'protocol retention only; native missing frames and saved-machine resize wait unverified',
          'products': results, 'equal': equal}
(a.out/'resize-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if equal and all(r['passed'] for r in results.values()) else 1)
