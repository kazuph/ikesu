#!/usr/bin/env python3
"""Compare measured mouse effects; missing captures never count as agreement."""
import argparse
import json
import re
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('directory', type=Path)
p.add_argument('case', choices=['wheel', 'modifier', 'pane-scroll', 'toast', 'arrows', 'workspace', 'sidebar-bars'])
a = p.parse_args()
root = a.directory


def cells(app, name):
    return json.loads((root/app/(name+'.json')).read_text())['Cells']


def text(app, name):
    return (root/app/(name+'.txt')).read_text()


def actions(app, method):
    saved = json.loads((root/app/'runtime-json.json').read_text())
    data = json.loads(saved['home/parity-api-actions.json'])
    return [row['response']['result'] for row in data if row['method'] == method]


def measure(app):
    if a.case == 'wheel':
        labels = []
        for name in ('first', 'up', 'down'):
            active = [c['Content'] for c in cells(app, name)[0]
                      if c.get('Content') in ('A', 'B', 'C') and c['Style']['Attrs'] & 1]
            assert len(active) == 1, (app, name, active)
            labels.append(active[0])
        assert labels in (['A', 'A', 'A'], ['A', 'C', 'A']), labels
        return labels
    if a.case == 'modifier':
        saved = json.loads((root/app/'runtime-json.json').read_text())
        data = b''.join(bytes.fromhex(s) for s in json.loads(saved['home/mouse-report-on.json']))
        assert data == b'\x1b[<2;18;8M\x1b[<34;23;10M\x1b[<2;23;10mq', data
        result = {'received': data.hex()}
        for name in ('shift-drag', 'plain-drag'):
            row = cells(app, name)[3][27:38]
            result[name] = [c['Style']['Bg'] for c in row]
        assert all(v is None for v in result['shift-drag']), result
        assert all(v is not None for v in result['plain-drag']), result
        return result
    if a.case == 'pane-scroll':
        values = [int(re.findall(r'SCROLL_LINE_(\d+)', text(app, name))[0])
                  for name in ('before', 'track-click', 'track-motion', 'track-release')]
        assert values == [161, 24, 132, 132], values
        return values
    if a.case == 'toast':
        before, right, left = [text(app, name).splitlines() for name in ('before', 'right-click', 'left-click')]
        assert '┌' in before[0][140:] and before[0] == right[0], (app, 'notification absent or dismissed')
        assert 'other' in ''.join(before[:4]), (app, 'wrong notification')
        assert 'Rename pane' in '\n'.join(right), (app, 'right click routing')
        assert '┌' not in left[0][140:], (app, 'left click did not dismiss')
        old = re.search(r'%([0-9]+) codex', before[1]).group(1)
        assert re.search(r'%([0-9]+) codex', right[1]).group(1) == old, (app, 'right click navigated')
        new = re.search(r'%([0-9]+) codex', left[1]).group(1)
        assert old != new, (app, 'left click did not navigate')
        return {'right_retained': True, 'right_pane_menu': True, 'left_navigated': True}
    if a.case == 'arrows':
        lists = [[tab['label'] for tab in row['tabs']] for row in actions(app, 'tab.list')]
        assert len(lists) == 3 and all(len(row) == 8 for row in lists), lists
        assert lists[0] == [letter*8 for letter in 'ABCDEFGH'], lists
        assert lists[1] != lists[0] and lists[2] != lists[1], lists
        return lists
    if a.case == 'workspace':
        values = {}
        for name in ('before', 'wheel-down', 'wheel-up', 'pressed', 'outside-release'):
            rows = text(app, name).splitlines()
            selected = [row[:26] for row in rows if row.startswith('▌') and ('parity' in row[:26] or 'other' in row[:26])]
            assert len(selected) == 1, (app, name, selected)
            values[name] = 'other' if 'other' in selected[0] else 'parity'
        assert values == {'before':'parity', 'wheel-down':'parity', 'wheel-up':'parity', 'pressed':'other', 'outside-release':'other'}, values
        orders = actions(app, 'workspace.list')
        order = orders[-2]['workspaces']
        assert [w['label'] for w in order] == ['parity', 'other'], order
        values['order'] = [w['label'] for w in order]
        values['reordered'] = [w['label'] for w in orders[-1]['workspaces']]
        assert values['reordered'] == ['other', 'parity'], values
        return values
    if a.case == 'sidebar-bars':
        values = {}
        for name in ('before', 'workspaces-bottom', 'workspaces-top', 'agents-bottom', 'agents-top', 'track-clicks'):
            rows = cells(app, name)
            # The 0.2.57 server displays no scrollbar in these panel columns.
            assert not any(c.get('Content') in ('█', '▕', '│')
                           for y in list(range(4, 21))+list(range(26, 43)) for c in rows[y][24:26]), name
            values[name] = {'workspace_scrollbar': False, 'agent_scrollbar': False}
        return values


result = {'case': a.case, 'status': 'FAIL'}
try:
    for app in ('herdr', 'gtx'):
        exit_info = json.loads((root/app/'ui-exit.json').read_text())
        assert exit_info['exit'] == 0
        if exit_info['server']:
            assert json.loads((root/app/'cleanup.json').read_text())['exit'] == 0
        result[app] = measure(app)
    assert result['herdr'] == result['gtx'], result
    result['status'] = 'PASS'
except (AssertionError, OSError, KeyError, ValueError, AttributeError) as error:
    result['error'] = str(error)
(root/'mouse-gate.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n')
print(json.dumps(result, ensure_ascii=False))
raise SystemExit(0 if result['status'] == 'PASS' else 1)
