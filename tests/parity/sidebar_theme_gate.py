#!/usr/bin/env python3
"""Check saved theme identity and every observed card/drag/menu frame."""
import json
from pathlib import Path
import subprocess
import sys
import tomllib

root = Path(sys.argv[1])
themes = {'catppuccin', 'catppuccin-latte', 'terminal', 'tokyo-night',
          'tokyo-night-day', 'dracula', 'nord', 'gruvbox', 'gruvbox-light',
          'one-dark', 'one-light', 'solarized', 'solarized-light'}
assert {p.name for p in root.iterdir() if p.is_dir()} == themes
helper = Path(__file__).with_name('region_compare.py')
screens = {'boot', 'new-workspace', 'first-active', 'menu-closed',
           'slim', 'full-again', 'drag-preview', 'reordered'}
for theme in sorted(themes):
    case = root/theme
    for runtime in ('herdr', 'gtx'):
        configs = json.loads((case/runtime/'config-files.json').read_text())
        applied = [tomllib.loads(text).get('theme', {}).get('name')
                   for path, text in configs.items() if path.endswith('/config.toml')]
        assert theme in applied, (theme, runtime, applied)
    for filename, rect, names in (
        ('region-comparison.json', '0,0,25,23', screens),
        ('menu-card-comparison.json', '0,0,4,23', {'workspace-menu', 'inactive-menu'}),
    ):
        subprocess.run([sys.executable, str(helper), str(case), '--rect', rect,
                        '--screens', *sorted(names), '--result', filename], check=True)
        rows = json.loads((case/filename).read_text())
        assert {row['screen'] for row in rows} == names, rows
        assert all(row['status'] == 'PASS' and row['difference_count'] == 0
                   for row in rows), (theme, rows)
print('13 persisted themes, 104 card/drag frames and 26 menu edge frames: PASS')
