#!/usr/bin/env python3
"""Check only the observed A14 controls and A15 collapsed-agent contract."""
import json
import re
import sys
from pathlib import Path

controls, agents = map(Path, sys.argv[1:])
proof = {}
for product in ('herdr', 'gtx'):
    directory = controls / product
    initial = json.loads((directory / 'initial.json').read_text())['Cells']
    changes = {}
    for name in ('density-click', 'sort-click', 'collapse-click'):
        after = json.loads((directory / (name + '.json')).read_text())['Cells']
        changes[name] = sum(a != b for row, other in zip(initial, after)
                            for a, b in zip(row[:25], other[:25]))
    assert all(count == 0 for count in changes.values()), (product, changes)
    proof[product] = {'A14_changed_sidebar_cells': changes}
    directory = agents / product
    text = (directory / 'collapsed.txt').read_text().splitlines()
    assert [line[:3] for line in text[24:28]] == [' ● ', ' c…', ' ○ ', ' c…']
    before = re.search(r'%([0-9]+) codex', text[1]).group(1)
    after = re.search(r'%([0-9]+) codex', (directory / 'agent-click.txt').read_text()).group(1)
    assert before != after, (product, before, after)
    proof[product]['A15_focus_before_after'] = [before, after]
    proof[product]['A15_card_rows'] = list(range(25, 29))

for name in ('collapsed', 'agent-click'):
    cells = [json.loads((agents / product / (name + '.json')).read_text())['Cells']
             for product in ('herdr', 'gtx')]
    left = [row[:3] for row in cells[0][24:28]]
    right = [row[:3] for row in cells[1][24:28]]
    assert left == right, (name, 'collapsed card cells/styles differ')
proof['A14'] = proof['A15'] = 'PASS'
print(json.dumps(proof, ensure_ascii=False, indent=2))
