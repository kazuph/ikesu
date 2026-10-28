#!/usr/bin/env python3
"""Compare every saved safe-copy pane frame, including search and selection."""
import json
from pathlib import Path
import re
import subprocess
import sys
root, steps = map(Path, sys.argv[1:])
names = re.findall(r'^cells @OUT@/([^/]+)\.json$', steps.read_text(), re.M)
assert names and len(names) == len(set(names)), names
for name in names:
    paths = [root/runtime/(name+'.json') for runtime in ('herdr', 'ikesu')]
    assert all(path.is_file() for path in paths), paths
    pair = [json.loads(path.read_text()) for path in paths]
    assert [(c['Cols'], c['Rows']) for c in pair] == [(pair[0]['Cols'], pair[0]['Rows'])]*2
    width, height = pair[0]['Cols'], pair[0]['Rows']
    # The pane begins at column 26; row 0 is the tab bar and the last
    # row is the status bar. Every pane cell, frame and hint is checked.
    assert width > 26 and height > 2
    result = name+'-comparison.json'
    subprocess.run([sys.executable, str(Path(__file__).with_name('region_compare.py')),
                    str(root), '--rect', f'26,1,{width-26},{height-2}',
                    '--screens', name, '--result', result], check=True)
    row, = json.loads((root/result).read_text())
    assert row['status'] == 'PASS' and row['difference_count'] == 0, (name, row)
print(f'{len(names)} safe copy pane frames: PASS')
