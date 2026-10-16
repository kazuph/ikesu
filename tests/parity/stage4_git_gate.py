#!/usr/bin/env python3
"""A19: real repository transitions, separate tokens and display-mode parity."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for case in ('custom-git-local', 'git-default-local'):
    saved = {}
    for product in ('herdr', 'gtx'):
        folder = root / case / product
        saved[product] = json.loads((folder / 'runtime-json.json').read_text())
        for phase, counts, diff in [('initial', '1\t0', '2\t1\ttracked.txt'),
                                    ('updated', '0\t1', '3\t1\ttracked.txt'), ('clean', '0\t0', '')]:
            actual = json.loads(saved[product][f'home/stage4-git-{phase}.json'])
            assert actual['status'] == counts and actual['diff'] == diff, (case, product, phase, actual)
    for phase, status, diff in [('initial', '↑1', '+2 -1'), ('updated', '↓1', '+3 -1'), ('clean', '', '')]:
        rows = [(root / case / product / (phase + '.txt')).read_text().splitlines()[4:6]
                for product in ('herdr', 'gtx')]
        fields = [[row[:25] for row in lines] for lines in rows]
        assert fields[0] == fields[1], (case, phase, fields)
        if case == 'custom-git-local':
            assert fields[0][0][1:].strip() == status and fields[0][1][1:].strip() == diff
        else:
            expected = ' '.join(x for x in ('main', status, diff) if x)
            assert fields[0][1][1:].strip() == expected, (phase, fields)
        proof[f'{case}/{phase}'] = fields[0]
for case in ('custom-git', 'git-default'):
    rows = []
    for product in ('herdr', 'gtx'):
        folder = root / case / product
        assert json.loads((folder / 'cleanup.json').read_text())['exit'] == 0
        actual = (folder / 'git-late.txt').read_text().splitlines()
        fields = [row[:25].replace('▌', ' ') for row in actual[4:8]]
        if case == 'custom-git':
            assert all(not row.strip() for row in fields), (case, product, fields)
        else:
            assert fields[3].strip() == 'main', (case, product, fields)
            assert all(not any(mark in row for mark in ('↑', '↓', '+', '-')) for row in (fields[1], fields[3]))
        rows.append(fields)
    proof[case] = {'counts_hidden_in_both_connected_displays': True}
proof['A19'] = 'PASS'
print(json.dumps(proof, ensure_ascii=False, indent=2))
