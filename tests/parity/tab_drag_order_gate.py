"""Verify the visible tab identities and release/cancellation results."""
from pathlib import Path
import json
import re
import sys
root = Path(sys.argv[1])
expected = {'two-before': 'AB', 'two-after': 'BA',
            'four-before': 'ABCD', 'four-after': 'BCDA',
            'release-last-target': 'CDAB', 'outside-motion': 'DABC', 'cancelled': 'DABC'}
mode = sys.argv[2] if len(sys.argv) > 2 else 'server'
assert mode in ('server', 'local'), 'unsupported attachment mode'
if mode == 'local':
    expected.update({'outside-motion': 'CDAB', 'cancelled': 'CDAB'})
for role in ['herdr', 'ikesu']:
    for name, order in expected.items():
        row = (root/role/(name+'.txt')).read_text().splitlines()[0]
        observed = ''.join(re.findall(r'\b[A-D]\b', row))
        assert observed == order, (role, name, observed, order, row)
for name in ('two-moving', 'four-moving'):
    rows = {role: json.loads((root/role/(name+'.json')).read_text())['Cells'][0]
            for role in ('herdr', 'ikesu')}
    positions = {role: [i for i, cell in enumerate(row) if i >= 26 and cell['Content'] == '│']
                 for role, row in rows.items()}
    if mode == 'server':
        assert positions['herdr'] and positions['ikesu'] == positions['herdr'], (name, positions)
        for x in positions['herdr']:
            assert rows['ikesu'][x] == rows['herdr'][x], (name, x, 'marker cell differs')
    else:
        # Preserve the earlier approved standalone trailing-marker improvement.
        assert positions['ikesu'], (name, 'standalone trailing marker absent')
print('PASS: two/four-tab identities, order and outside cancellation match')
