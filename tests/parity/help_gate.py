#!/usr/bin/env python3
"""Require every measured help frame and post-close shell input to match."""
import json
from pathlib import Path
import sys

work = Path(sys.argv[1])
for scenario, filename, screens in [
    ('help-complete', 'help-comparison.json', {f'page-{i}.json' for i in range(8)}),
    ('help-mouse', 'help-comparison.json',
     {'body-click.json', 'footer-click.json', 'gutter-drag.json', 'gutter-press.json'}),
    ('help-size', 'settings-size-comparison.json',
     {'w100-h25.json', 'w160-h45.json', 'w20-h25.json', 'w40-h25.json',
      'w60-h15.json', 'w60-h25.json', 'w60-h9.json', 'w64-h25.json',
      'w76-h25.json', 'w79-h25.json', 'w80-h25.json'}),
    ('help-compact', 'settings-size-comparison.json',
     {f'w{w}-h25.json' for w in (20, 24, 25, 26, 27, 28, 29, 30, 31, 32, 34, 36, 40)} |
     {f'w60-h{h}.json' for h in (8, 9, 10, 11, 12, 13, 14)}),
]:
    results = json.loads((work/f'parity-{scenario}-help-detail'/filename).read_text())
    assert {r['screen'] for r in results} == screens, (scenario, 'missing frames')
    assert all(r['status'] == 'PASS' and r['difference_count'] == 0 for r in results), scenario

closed = work/'parity-help-close-help-detail'
proof = {}
for who in ('herdr', 'ikesu'):
    root = closed/who
    proof[who] = {
        'shell_marker_count': (root/'shell-input.txt').read_text().count('HELP_CLOSE_MARK'),
        'header_help_count': (root/'header-close.txt').read_text().count('keybinds'),
        'outside_help_count': (root/'outside.txt').read_text().count('keybinds'),
    }
    assert proof[who] == {'shell_marker_count': 2, 'header_help_count': 0,
                          'outside_help_count': 0}, (who, proof[who])
(closed/'close-proof.json').write_text(json.dumps(proof, indent=2)+'\n')
background = json.loads((work/'parity-overlay-background-fixed/region-comparison.json').read_text())
assert len(background) == 9 and all(r['status'] == 'PASS' for r in background), background
print('PASS: all help frames, close routing, and modal background')
