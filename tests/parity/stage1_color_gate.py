#!/usr/bin/env python3
"""Check held real host queries, fragmented replies and a bounded Escape wait."""
import json
from pathlib import Path
import sys

results = []
for argument, expected, screen in [(sys.argv[1], b'END', 'complete'),
                                   (sys.argv[2], b'\x1b', 'escape')]:
    root = Path(argument)
    for name in ['herdr', 'ikesu']:
        folder = root/name
        clock = json.loads((folder/(screen+'.json')).read_text())
        records = [json.loads(line) for line in (folder/'received.jsonl').read_text().splitlines()]
        records = [row for row in records if row['unix_ns']//1000000 <= clock['CapturedUnixMs']]
        data = b''.join(bytes.fromhex(row['hex']) for row in records)
        held = 'held-color-report "11;?"' in (folder/'run.log').read_text()
        results.append({'case': root.name, 'product': name, 'pass': held and data == expected,
                        'held_query': held, 'expected_hex': expected.hex(), 'received_hex': data.hex(),
                        'first_delivery_ms': records[0]['unix_ns']/1000000-clock['LastSendUnixMs'] if records else None})
target = Path(sys.argv[1])/'color-gate.json'
target.write_text(json.dumps(results, indent=2)+'\n')
print(json.dumps(results, indent=2))
raise SystemExit(0 if all(row['pass'] for row in results) else 1)
