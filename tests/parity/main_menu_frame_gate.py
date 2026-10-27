#!/usr/bin/env python3
"""Preserve and record Main item differences, but require every outside cell to match."""
from pathlib import Path
import json
import sys

root = Path(sys.argv[1])
frames = [json.loads((root / who / 'open.json').read_text()) for who in ('herdr', 'ikesu')]
boxes = []
items = []
for frame in frames:
    cells = frame['Cells']
    corners = [(x, y) for y, row in enumerate(cells) for x, c in enumerate(row[:2]) if c['Content'] == '┌']
    assert len(corners) == 1, 'Main must have one left-anchored menu'
    x, y = corners[0]
    right = next(xx for xx in range(x + 1, frame['Cols']) if cells[y][xx]['Content'] == '┐')
    bottom = next(yy for yy in range(y + 1, frame['Rows']) if cells[yy][x]['Content'] == '└')
    footer_rows = [yy for yy, row in enumerate(cells)
                   if '[menu]' in ''.join(c['Content'] for c in row[:right+1])]
    assert len(footer_rows) == 1 and bottom == footer_rows[0]-1, 'Main must sit directly above its footer'
    boxes.append((x, y, right, bottom))
    items.append([''.join(c['Content'] for c in cells[yy][x+1:right]).strip() for yy in range(y+1, bottom)])
assert len({(f['Cols'], f['Rows']) for f in frames}) == 1, 'frame sizes differ'
diffs = []
for y in range(frames[0]['Rows']):
    for x in range(frames[0]['Cols']):
        if any(left <= x <= right and top <= y <= bottom for left, top, right, bottom in boxes):
            continue
        if frames[0]['Cells'][y][x] != frames[1]['Cells'][y][x]:
            diffs.append((x, y))
result = {'boxes':boxes, 'herdr_items':items[0], 'ikesu_items':items[1], 'outside_differences':diffs,
          'item_contents_and_order': 'record only; excluded by the user boundary'}
(root / 'main-menu-frame-gate.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n')
assert not diffs, f'Main outside cells differ: {diffs}'
for name in ('outside', 'input', 'escaped'):
    h, g = [json.loads((root / who / (name+'.json')).read_text()) for who in ('herdr', 'ikesu')]
    assert h['Cells'] == g['Cells'], f'Main {name} did not restore the identical frame/input'
assert (root/'herdr/input.txt').read_text().count('SERVER_MENU_INPUT') == 2, 'shell did not receive input'
print('PASS: Main anchored menus recorded; all outside and closed frames match; shell received input')
