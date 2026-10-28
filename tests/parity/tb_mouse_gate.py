#!/usr/bin/env python3
"""Require exact application-side mouse bytes for the captured input stages."""
import argparse
import json
import re
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
records = {}
for name in ('herdr', 'ikesu'):
    runtime = json.loads((a.out/name/'runtime-json.json').read_text())
    records[name] = json.loads(runtime['home/tb-mouse-probe.json'])
streams = {name: b''.join(bytes.fromhex(row['received_hex']) for row in rows) for name, rows in records.items()}
reference = streams['herdr']
reports = re.findall(rb'\x1b\[<(\d+);\d+;\d+[Mm]', reference)
valid = (len(records['herdr']) == 7
         and [int(code) for code in reports] == [35, 66, 67, 35, 0, 32, 0, 35, 66, 67])
passed = valid and streams['ikesu'] == reference
result = {'reference_has_all_ten_mouse_events': valid, 'exact_bytes_equal': passed,
          'scope': 'mouse event bytes, order, and coordinates; keyboard delimiters can overtake deferred hover events',
          'stream_hex': {name: data.hex() for name, data in streams.items()}, 'records': records}
(a.out/'mouse-gate.json').write_text(json.dumps(result, indent=2)+'\n')
print(json.dumps({'reference_has_all_ten_mouse_events': valid, 'exact_bytes_equal': passed}))
raise SystemExit(0 if passed else 1)
