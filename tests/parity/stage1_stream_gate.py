#!/usr/bin/env python3
"""Assert complete paste framing and exact UTF-8 bytes from the real pane."""
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
expected = b'\x1b[200~first\nsecond\n\x1b[201~' + '日\x1b日END'.encode()
results = {}
for name in ['herdr', 'ikesu']:
    records = [json.loads(line) for line in (root/name/'received.jsonl').read_text().splitlines()]
    received = b''.join(bytes.fromhex(record['hex']) for record in records)
    results[name] = {'received_hex': received.hex(), 'expected_hex': expected.hex(),
                     'pass': received == expected}
(root/'stream-gate.json').write_text(json.dumps(results, indent=2) + '\n')
print(json.dumps(results, indent=2))
raise SystemExit(0 if all(result['pass'] for result in results.values()) else 1)
