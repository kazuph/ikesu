#!/usr/bin/env python3
"""Compare mobile list actions using measured header, list and pane regions."""
import argparse
import json
import subprocess
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory')
a = p.parse_args()
root = Path(a.directory)
helper = Path(__file__).with_name('region_compare.py')
expected = {'new-workspace', 'new-tab', 'first-tab', 'first-workspace',
            'second-workspace', 'workspace-detail', 'section-collapse',
            'settings', 'heading', 'blank-header', 'close-top'}
assert {p.name for p in root.iterdir() if p.is_dir()} == expected
for case in sorted(root.iterdir()):
    if not case.is_dir():
        continue
    print(case.name, flush=True)
    subprocess.run(['python3', str(helper), str(case), '--rect', '0,0,60,2',
                    '--screens', 'before', 'after', '--result', 'header-comparison.json'], check=True)
    if case.name in ('blank-header', 'heading', 'section-collapse'):
        subprocess.run(['python3', str(helper), str(case), '--rect', '0,0,60,25',
                        '--screens', 'after', '--result', 'list-comparison.json'], check=True)
    elif case.name not in ('new-tab', 'settings'):
        subprocess.run(['python3', str(helper), str(case), '--rect', '0,2,60,1',
                        '--screens', 'after', '--result', 'pane-comparison.json'], check=True)
    else:
        title = 'new tab' if case.name == 'new-tab' else ' settings'
        subprocess.run(['python3', str(helper.with_name('navigator_compare.py')), str(case),
                        '--title', title, '--result', 'overlay-comparison.json'], check=True)
    for path in case.glob('*-comparison.json'):
        rows = json.loads(path.read_text())
        if path.name == 'overlay-comparison.json':
            rows = [row for row in rows if row['screen'] == 'after.json']
            assert len(rows) == 1, (path, rows)
        assert rows and all(row['status'] == 'PASS' and row['difference_count'] == 0
                            for row in rows), (path, rows)
