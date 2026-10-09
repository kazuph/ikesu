#!/usr/bin/env python3
"""Compare all saved action-bar rows, including copy transitions and resizing."""
import json
from pathlib import Path
import re
import subprocess
import sys

root, steps = map(Path, sys.argv[1:])
names = re.findall(r'^cells @OUT@/([^/]+)\.json$', steps.read_text(), re.M)
assert names and len(names) == len(set(names)), names
for name in names:
    pair = [json.loads((root/runtime/(name+'.json')).read_text()) for runtime in ('herdr', 'gtx')]
    dimensions = [(data['Cols'], data['Rows']) for data in pair]
    assert dimensions[0] == dimensions[1], (name, dimensions)
    width, height = dimensions[0]
    assert width > 26 and height > 1
    result = name+'-actionbar-comparison.json'
    subprocess.run([sys.executable, str(Path(__file__).with_name('region_compare.py')),
                    str(root), '--rect', f'26,{height-1},{width-26},1',
                    '--screens', name, '--result', result], check=True)
    row, = json.loads((root/result).read_text())
    assert row['status'] == 'PASS' and row['difference_count'] == 0, (name, row)
print(f'{len(names)} complete action-bar rows: PASS')
