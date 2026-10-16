#!/usr/bin/env python3
"""Measure real delivery times and compare bytes before observer shutdown."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
results = []
for mode in ['local', 'server']:
    for case in ['timing', 'escape-plain', 'escape-mouse']:
        measured = {}
        for name in ['herdr', 'gtx']:
            folder = root/mode/case/name
            clock = json.loads((folder/'escape.json').read_text())
            capture = folder/('complete.json' if case == 'timing' else 'escape.json')
            cutoff = json.loads(capture.read_text())['CapturedUnixMs'] * 1000000 if capture.exists() else (folder/'complete.txt').stat().st_mtime_ns
            records = [json.loads(line) for line in (folder/'received.jsonl').read_text().splitlines()]
            records = [row for row in records if row['unix_ns'] <= cutoff]
            measured[name] = {'hex': ''.join(row['hex'] for row in records),
                              'first_delivery_ms': records[0]['unix_ns']/1000000-clock['LastSendUnixMs'] if records else None,
                              'cutoff_ns': cutoff, 'records': records}
        results.append({'mode': mode, 'case': case, 'same_bytes': measured['herdr']['hex'] == measured['gtx']['hex'],
                        'measurements': measured})
(root/'timing-measurements.json').write_text(json.dumps(results, indent=2)+'\n')
for row in results:
    print(row['mode'], row['case'], 'same_bytes='+str(row['same_bytes']),
          {name: value['first_delivery_ms'] for name, value in row['measurements'].items()})
