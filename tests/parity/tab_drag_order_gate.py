"""Verify the visible tab identities and release/cancellation results."""
from pathlib import Path
import re
import sys
root = Path(sys.argv[1])
expected = {'two-before': 'AB', 'two-after': 'BA',
            'four-before': 'ABCD', 'four-after': 'BCDA',
            'release-last-target': 'CDAB', 'outside-motion': 'DABC', 'cancelled': 'DABC'}
for role in ['herdr', 'gtx']:
    for name, order in expected.items():
        row = (root/role/(name+'.txt')).read_text().splitlines()[0]
        observed = ''.join(re.findall(r'\b[A-D]\b', row))
        assert observed == order, (role, name, observed, order, row)
print('PASS: two/four-tab identities, order and outside cancellation match')
