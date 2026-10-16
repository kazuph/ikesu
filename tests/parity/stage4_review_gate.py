#!/usr/bin/env python3
"""First-review regressions: real collapsed viewports and stripped row heights."""
import json
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
proof = {}


def lines(case, product, name):
    return (root / case / product / (name + '.txt')).read_text().splitlines()


def focused(screen):
    return 'p' + re.search(r'%([0-9]+) codex', screen[1]).group(1)


def fixture(case, product, name):
    data = json.loads((root / case / product / 'runtime-json.json').read_text())
    return json.loads(data['home/' + name + '.json'])


for product in ('herdr', 'gtx'):
    records = fixture('review-overflow', product, 'stage4-review-overflow')
    assert len(records) == 30
    by_workspace = {int(r['workspace_id'][1:]): r['pane_id'] for r in records}
    screens = {name: lines('review-overflow', product, name) for name in
               ('inherited', 'inherited-click', 'upper-end', 'tail-click', 'agent-down', 'lower-click')}
    for before, after in [('inherited', 'inherited-click'), ('upper-end', 'tail-click')]:
        displayed = int(re.match(r'\s*(\d+)', screens[before][0]).group(1))
        assert focused(screens[after]) == by_workspace[displayed], (product, before, displayed, focused(screens[after]))
    last = int(records[-1]['workspace_id'][1:])
    assert int(re.match(r'\s*(\d+)', screens['upper-end'][22]).group(1)) == last, (product, 'tail unreachable')
    assert not screens['upper-end'][23][:3].strip(), (product, 'divider occupied')
    upper = lambda name: [line[:3] for line in screens[name][:24]]
    assert upper('tail-click') == upper('agent-down'), (product, 'lower wheel moved upper')
    assert upper('agent-down') == upper('lower-click'), (product, 'lower click moved upper')
    cards = lambda name: [line[:3] for line in screens[name][24:43]]
    assert cards('agent-down')[:-1] == cards('tail-click')[1:], (product, 'not one physical row')
    assert focused(screens['lower-click']) == records[0]['pane_id'], (product, 'partial card click')
    stripped = {name: lines('review-stripped', product, name) for name in ('empty', 'task', 'empty-again', 'clicked')}
    for name in ('empty', 'empty-again', 'clicked'):
        assert [line[:25].strip() for line in stripped[name][26:29]] == ['SECOND_ROW', '', ''], (product, name)
    assert [line[:25].strip() for line in stripped['task'][26:29]] == ['Task', 'SECOND_ROW', ''], product
    second = fixture('review-stripped', product, 'stage4-review-stripped')['root_pane']['pane_id']
    assert focused(stripped['clicked']) == second, (product, 'next agent click')
    proof[product] = {'workspaces': 30, 'upper_height': 23, 'tail': last,
                      'inherited_and_tail_click': 'PASS', 'lower_wheel_rows': 1,
                      'lower_click': 'PASS', 'stripped_heights': [0, 1, 0], 'next_agent_click': 'PASS'}
proof['review_findings'] = 'PASS'
print(json.dumps(proof, ensure_ascii=False, indent=2))
