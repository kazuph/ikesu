#!/usr/bin/env python3
"""Compare the real two-pane fixture's top border colors after focus selection."""
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
measurements = {}
for who in ('herdr', 'gtx'):
    data = json.loads((root / who / 'after.json').read_text())
    measurements[who] = [
        {'x': x, 'y': 1, 'style': cell['Style']}
        for x, cell in enumerate(data['Cells'][1]) if cell['Content'] == '┌'
    ]
status = 'NOT_OBSERVED' if any(len(v) != 2 for v in measurements.values()) else (
    'PASS' if measurements['herdr'] == measurements['gtx'] else 'DIFFERENT_FOCUS')
(root / 'focus-comparison.json').write_text(json.dumps({'status': status, **measurements}, indent=2) + '\n')
print('pane-focus', status)
raise SystemExit(0 if status == 'PASS' else 1)
