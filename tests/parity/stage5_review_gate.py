#!/usr/bin/env python3
"""Compare the observed restore-title cells and the real failed-close response."""
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
checks = []
for case in sorted(root.glob('title-*')):
    if not case.is_dir():
        continue
    for width in (34, 40, 50):
        rows = [json.loads((case / who / f'width-{width}.json').read_text())['Cells'][1][26:] for who in ('herdr', 'gtx')]
        checks.append({'case': f'{case.name}/{width}', 'pass': rows[0] == rows[1], 'cells': width})
proof = json.loads((root / 'failure/gtx/close-proof.json').read_text())
before = proof['before']['result']['workspaces']
after = proof['after']['result']['workspaces']
checks.append({'case': 'failed close reports error and keeps workspace', 'pass': bool(proof['closed'].get('error', {}).get('message')) and len(before) == len(after) == 1 and before[0]['workspace_id'] == after[0]['workspace_id']})
(root / 'review-gate.json').write_text(json.dumps(checks, indent=2) + '\n')
print(f"{sum(c['pass'] for c in checks)}/{len(checks)} PASS")
sys.exit(0 if len(checks) == 13 and all(c['pass'] for c in checks) else 1)
