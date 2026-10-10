#!/usr/bin/env python3
"""Prove observed cancellation UI and disappearance of the exact real process group."""
import argparse
import json
from pathlib import Path
import re
import sys

p = argparse.ArgumentParser()
p.add_argument('root', type=Path)
p.add_argument('--theme', choices=('default', 'tokyo-night'), default='default')
a = p.parse_args()
root = a.root
red = {'default': {'R':243,'G':139,'B':168,'A':255},
       'tokyo-night': {'R':247,'G':118,'B':142,'A':255}}[a.theme]
proof = {}
frames = {}
for who in ('herdr','gtx'):
    case = root/who
    runtime = json.loads((case/'runtime-json.json').read_text())
    process = json.loads(runtime['home/parity-job-processes.json'])
    started = json.loads(runtime['home/parity-job.json'])['result']['job']
    reply = json.loads(runtime['home/parity-job-cancel.json'])
    assert process['job'] == started and not process['group_alive'], process
    assert reply['exit'] == 0, reply
    logs = list(json.loads((case/'job-logs.json').read_text()).values())
    assert len(logs) == 1
    runner = int(re.search(r'^runner_pid: (\d+)$',logs[0],re.M)[1])
    assert runner == process['runner_pid']
    assert 'finished_unix_ms:' not in logs[0], 'observed cancelled log has no exit footer'
    cleanup = json.loads((case/'cleanup.json').read_text())
    assert cleanup['exit'] == 0 and any('"type":"ok"' in line for line in cleanup['replies'])
    frames[who] = {name:json.loads((case/(name+'.json')).read_text())
                   for name in ('cancelling-later','cancelled')}
    for name, frame in frames[who].items():
        rows = [''.join(cell['Content'] for cell in row[:25]) for row in frame['Cells']]
        matched = [y for y,row in enumerate(rows) if 'PARITY_' in row]
        assert len(matched) == 1
        y = matched[0]
        assert rows[y].split()[0] == 'done'
        assert frame['Cells'][y][1]['Style']['Fg'] == red
        if name == 'cancelling-later':
            assert rows[y].split()[2] == '-'
        else:
            assert process['checked_unix_ms'] <= frame['CapturedUnixMs']
    proof[who] = {'process_group':process,'cancel_reply':reply,'cleanup':cleanup}
comparisons = []
for name in ('cancelling-later','cancelled'):
    h,g = frames['herdr'][name],frames['gtx'][name]
    assert (h['Cols'],h['Rows']) == (g['Cols'],g['Rows'])
    width = 25 if name == 'cancelling-later' else h['Cols']
    differences = [{'x':x,'y':y,'herdr':h['Cells'][y][x],'gtx':g['Cells'][y][x]}
                   for y in range(h['Rows']) for x in range(width)
                   if h['Cells'][y][x] != g['Cells'][y][x]]
    comparisons.append({'screen':name,'rect':[0,0,width,h['Rows']], 'differences':differences})
    print(name, 'PASS' if not differences else 'DIFFERENT',len(differences))
(root/'cancel-proof.json').write_text(json.dumps({'proof':proof,'comparisons':comparisons},indent=2)+'\n')
assert all(not row['differences'] for row in comparisons), 'raw observed cancellation cells differ'
