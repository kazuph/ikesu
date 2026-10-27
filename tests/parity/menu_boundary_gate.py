#!/usr/bin/env python3
"""Verify observed menu clamping and anchoring without comparing menu items."""
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
selected = []
for who in ('herdr', 'ikesu'):
    case = root/'parity-menu-bottom-menu-detail'/who
    bottom = json.loads((case/'pane-bottom.json').read_text())
    next_row = json.loads((case/'pane-bottom-next.json').read_text())
    assert bottom['Cells'] == next_row['Cells'], who
    matches = []
    for row in bottom['Cells']:
        text = ''.join(cell['Content'] for cell in row)
        if 'Close pane' in text:
            x = text.index('Close pane')
            matches.append(row[x]['Style'])
    assert len(matches) == 1, (who, matches)
    selected.append(matches[0])
assert selected[0] == selected[1] and selected[0]['Attrs'] == 1, selected
print('context menu final selection: PASS')

def box(data):
    cells = data['Cells']
    corners = [(x, y) for y, row in enumerate(cells) for x, cell in enumerate(row[:2])
               if cell['Content'] == '┌']
    assert len(corners) == 1, corners
    x, y = corners[0]
    right = next(xx for xx in range(x+1, data['Cols']) if cells[y][xx]['Content'] == '┐')
    bottom = next(yy for yy in range(y+1, data['Rows']) if cells[yy][x]['Content'] == '└')
    return [x, y, right-x+1, bottom-y+1], cells[y][x]['Style']

for screen in ('open', 'small', 'restored'):
    panels = [box(json.loads((root/'parity-menu-resize-menu-detail'/who/(screen+'.json')).read_text()))
              for who in ('herdr', 'ikesu')]
    assert panels[0] == panels[1], (screen, panels)
for who in ('herdr', 'ikesu'):
    closed = (root/'parity-menu-resize-menu-detail'/who/'closed.txt').read_text()
    assert closed.count('MENU_RESIZE_CLOSED') == 2, (who, closed)
print('global menu resize anchor and restored input: PASS')
