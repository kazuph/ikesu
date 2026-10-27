"""Check real application bytes and retained tab drag against the reference."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
captures = {}
for role in ('herdr', 'ikesu'):
    saved = json.loads((root/role/'runtime-json.json').read_text())
    captures[role] = {}
    for mode in ('off', 'on'):
        chunks = json.loads(saved['home/mouse-report-'+mode+'.json'])
        data = b''.join(bytes.fromhex(chunk) for chunk in chunks)
        captures[role][mode] = data
        text = re.sub(rb'\x1b\[<[0-9;]+[Mm]', b'', data)
        want = b'OFF_ENDOFF_OUT\x1bq' if mode == 'off' else b'NEG_ENDOUT_ENDq'
        assert text == want, (role, mode, text, want)
        if mode == 'on':
            assert b'\x1b[<' in data, (role, 'application did not receive valid mouse reports')
    for screen, want in (('tab-negative-release', 'AB'), ('tab-return-release', 'BA')):
        row = (root/role/(screen+'.txt')).read_text().splitlines()[0]
        assert ''.join(re.findall(r'\b[AB]\b', row)) == want, (role, screen, row)
assert captures['ikesu'] == captures['herdr'], captures
print('PASS: no mouse tails as text, exact application reports, drag retained until valid release')
