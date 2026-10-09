#!/usr/bin/env python3
"""Measure saved terminal cells; each equality is limited to its named field."""
import argparse
import json
from pathlib import Path
import re
import unicodedata


def cells(text):
    result = []
    for char in text:
        if unicodedata.combining(char):
            if result:
                result[-1] += char
            continue
        result.append(char)
        if unicodedata.east_asian_width(char) in ('W', 'F'):
            result.append('')
    return result


def measure(path):
    lines = path.read_text().splitlines()
    grid = [cells(line) for line in lines]
    boxes = []
    for y, row in enumerate(grid):
        for x, char in enumerate(row):
            if char != '┌':
                continue
            for right in range(x+1, len(row)):
                if row[right] != '┐':
                    continue
                for bottom in range(y+1, len(grid)):
                    lower = grid[bottom]
                    if len(lower) > right and lower[x] == '└' and lower[right] == '┘':
                        boxes.append({'x':x, 'y':y, 'width':right-x+1, 'height':bottom-y+1})
                        break
                break
    return {'rows':len(lines), 'widths':[len(row) for row in grid], 'boxes':boxes,
            'pane_titles': [re.findall(r'(?:ZOOM )?%\d+ [^─│┐]*', line) for line in lines if re.search(r'%\d+', line)],
            'bottom': lines[-1] if lines else '',
            'help_rows': [line.strip(' │▕▐') for line in lines if 'prefix+' in line],
            'input_values': [token for token in ('abcZ', 'xyzZ', 'echo PASTE_RENAME', 'test-name') if any(token in line for line in lines)]}

p = argparse.ArgumentParser()
p.add_argument('directory')
a = p.parse_args()
root = Path(a.directory)
rows = []
for hp in sorted((root/'herdr').glob('*.txt')):
    if hp.name == 'server-pid.txt':
        continue
    gp = root/'gtx'/hp.name
    if not gp.exists():
        rows.append({'screen':hp.name, 'status':'NOT_RUN'})
        continue
    h, g = measure(hp), measure(gp)
    rows.append({'screen':hp.name, 'herdr':h, 'gtx':g,
                 'equal_fields':{key:h[key] == g[key] for key in h}})
(root/'measurements.json').write_text(json.dumps(rows, ensure_ascii=False, indent=2)+'\n')
for row in rows:
    print(row['screen'], json.dumps(row.get('equal_fields', {'status':'NOT_RUN'})))
