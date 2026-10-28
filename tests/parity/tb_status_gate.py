#!/usr/bin/env python3
"""Check actual terminal status ordering, latest pixels and PTY input."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
p.add_argument('--standalone', action='store_true')
a = p.parse_args()
reference = json.loads((a.out/'reference.json').read_text())
results = {}
for product in ('herdr', 'ikesu'):
    root = a.out/product
    raw = (root/'terminal.raw').read_bytes()
    log = (root/'run.log').read_text()
    runtime = json.loads((root/'runtime-json.json').read_text())
    captured = json.loads(runtime['home/tb-status.json'])
    queries = [int(t) for t in re.findall(r'status-query at_unix_us=(\d+)', log)]
    replies = [int(t) for t in re.findall(r'status-reply at_unix_us=(\d+) error=<nil>', log)]
    uploads = re.findall(rb'\x1b_Ga=t,[^;]*;(.*?)\x1b\\', raw, re.S)
    pixels = base64.b64decode(uploads[-1], validate=True).hex() if uploads else None
    passed = (captured['received_hex'] == b'q'.hex() and pixels == captured['final_pixel_hex']
              and raw.count(b'\x1b[5n') == len(queries) and 'TIMEOUT' not in log)
    intervals = [(right-left)/1000 for left, right in zip(queries, queries[1:])]
    if a.standalone:
        ordering = not queries
    elif reference['status_reply_delay'] == 'drop':
        ordering = len(queries) >= 2 and not replies and all(ms >= 1000 for ms in intervals)
    else:
        ordering = (len(queries) >= 2 and len(replies) >= len(queries)-1
                    and all(queries[i+1] >= replies[i] for i in range(len(queries)-1)))
    results[product] = {'passed': passed and ordering, 'queries': len(queries), 'uploads': len(uploads),
                        'query_intervals_ms': intervals, 'reply_order_valid': ordering,
                        'last_pixels_hex': pixels, 'received_hex': captured['received_hex']}
coalesced = a.standalone or results['ikesu']['uploads'] <= results['herdr']['uploads']
report = {'scope': 'isolated PTY protocol; exact one-second deadline is covered by the Go boundary test; native display unverified',
          'products': results, 'coalesced_no_worse_than_reference': coalesced}
(a.out/'status-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if coalesced and all(r['passed'] for r in results.values()) else 1)
