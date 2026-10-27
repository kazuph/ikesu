#!/usr/bin/env python3
"""Compare medium replies, displayed pixels, and owned resource lifetimes."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
results = {}
for product in ('herdr', 'ikesu'):
    root = a.out/product
    runtime = json.loads((root/'runtime-json.json').read_text())
    records = json.loads(runtime['home/tb-media.json'])
    expected = [(medium, action) for medium in ('f', 't', 's') for action in ('q', 'T')]
    replies = [(row['medium'], row['action']) for row in records] == expected
    lifetime = replies
    for row in records:
        replies &= bytes.fromhex(row['reply_hex']).endswith(b';OK\x1b\\')
        lifetime &= row['exists_after'] == (row['medium'] == 'f')
        lifetime &= row.get('exists_after_fixture_cleanup') is False
    raw = (root/'terminal.raw').read_bytes()
    images, placements = {}, {}
    cursor = (0, 0)
    for match in re.finditer(rb'\x1b\[(\d+);(\d+)H|\x1b_G(.*?)\x1b\\', raw, re.S):
        if match[1] is not None:
            cursor = (int(match[2])-1, int(match[1])-1)
            continue
        header, _, payload = match[3].partition(b';')
        keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
        action, image = keys.get(b'a', b't'), int(keys.get(b'i', b'0'))
        if action == b't':
            assert keys.get(b'm', b'0') == b'0' and keys.get(b't', b'd') == b'd'
            images[image] = base64.b64decode(payload, validate=True).hex()
        elif action == b'p':
            placements[(image, int(keys.get(b'p', b'0')))] = [*cursor, int(keys[b'c']), int(keys[b'r'])]
        elif action == b'd':
            placements = {key: value for key, value in placements.items() if key[0] != image}
    displayed = sorted([{'geometry': value, 'pixels': images[key[0]]} for key, value in placements.items()], key=lambda row: row['geometry'])
    pixels = len(displayed) == 3 and all(row['pixels'] == 'ff000000ff00' and row['geometry'][2:] == [4, 2] for row in displayed)
    results[product] = {'passed': replies and lifetime and pixels, 'replies': replies,
                        'lifetimes': lifetime, 'displayed': displayed, 'records': records}
equal = results['herdr']['displayed'] == results['ikesu']['displayed']
report = {'scope': 'actual isolated medium inputs and outer pixels; native display unverified',
          'products': results, 'equal_display': equal}
(a.out/'media-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps({product: {'passed': row['passed'], 'lifetimes': row['lifetimes'], 'displayed': row['displayed']} for product, row in results.items()}))
raise SystemExit(0 if equal and all(row['passed'] for row in results.values()) else 1)
