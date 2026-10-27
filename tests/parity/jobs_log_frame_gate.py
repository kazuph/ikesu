#!/usr/bin/env python3
"""Prove each real log's displayed bytes, then compare all remaining frame fields."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
proof = {}
frames = {}
verified = {}
for who in ('herdr','ikesu'):
    case = root/who
    logs = list(json.loads((case/'job-logs.json').read_text()).values())
    assert len(logs) == 1
    log = logs[0]
    started = int(re.search(r'^started_unix_ms: (\d+)$',log,re.M)[1])
    finished = int(re.search(r'^finished_unix_ms: (\d+)$',log,re.M)[1])
    prefix = log.split('\nfinished_unix_ms:')[0]
    frames[who], verified[who], proof[who] = {}, {}, []
    for name, echo in (('opened',''),('q','q'),('typed','qLOG_INPUT')):
        frame = json.loads((case/(name+'.json')).read_text())
        frames[who][name] = frame
        lines = [''.join(cell['Content'] for cell in row) for row in frame['Cells']]
        titles = [y for y,line in enumerate(lines) if '┌job log: PARITY_JOB' in line]
        assert len(titles) == 1
        top = titles[0]
        left = lines[top].index('┌job log: PARITY_JOB')
        right = lines[top].index('┐',left)
        bottoms = [y for y in range(top+1,frame['Rows']) if frame['Cells'][y][left]['Content'] == '└']
        assert len(bottoms) == 1
        bottom = bottoms[0]
        width = right-left-2
        expected = prefix.split('\n')
        expected[-1] += echo
        assert all(line.isascii() and len(line) < width for line in expected), 'fixture exceeds verified unwrapped ASCII log'
        assert len(expected) <= bottom-top-1
        expected += ['']*(bottom-top-1-len(expected))
        for y,line in enumerate(expected,top+1):
            actual = ''.join(cell['Content'] for cell in frame['Cells'][y][left+1:right-1])
            assert actual == line.ljust(width), (who,name,y,actual,line)
        cells = {(x,y) for y in range(top+1,bottom) for x in range(left+1,right-1)}
        rows = [y for y,line in enumerate(lines) if 'PARITY_' in line[:25]]
        assert len(rows) == 1
        row = rows[0]
        painted = max(frame['CellChangedUnixMs'][row][13:17])
        assert started <= painted <= frame['CapturedUnixMs'] < finished
        elapsed = ''.join(cell['Content'] for cell in frame['Cells'][row][13:17]).strip()
        assert elapsed == str((painted-started)//1000)+'s'
        cells.update((x,row) for x in range(13,17))
        verified[who][name] = cells
        proof[who].append({'screen':name,'log_prefix':prefix,'echo':echo,'elapsed':elapsed,
                           'painted_unix_ms':painted,'rect':[left,top,right-left+1,bottom-top+1]})
comparisons = []
for name in ('opened','q','typed'):
    h,g = frames['herdr'][name],frames['ikesu'][name]
    assert (h['Cols'],h['Rows']) == (g['Cols'],g['Rows'])
    assert verified['herdr'][name] == verified['ikesu'][name]
    differences = []
    for y in range(h['Rows']):
        for x in range(h['Cols']):
            for key in ('Content','Width','Style'):
                if key == 'Content' and (x,y) in verified['herdr'][name]:
                    continue
                if h['Cells'][y][x][key] != g['Cells'][y][x][key]:
                    differences.append({'x':x,'y':y,'field':key,'herdr':h['Cells'][y][x][key],'ikesu':g['Cells'][y][x][key]})
    comparisons.append({'screen':name,'differences':differences})
    print(name,'PASS' if not differences else 'DIFFERENT',len(differences))
(root/'log-frame-proof.json').write_text(json.dumps({'proof':proof,'comparisons':comparisons},indent=2)+'\n')
assert all(not row['differences'] for row in comparisons), 'log viewer frames differ outside independently verified bytes'
