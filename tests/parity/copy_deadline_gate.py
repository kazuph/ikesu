#!/usr/bin/env python3
"""Verify the SPEC two-second boundary using actual input and capture times."""
from pathlib import Path
import json
import sys

root = Path(sys.argv[1])
references = {}
result = {}
for who in ('herdr', 'ikesu'):
    files = sorted((root / who).glob('time-*.json'), key=lambda p: int(p.stem.split('-')[-1]))
    assert files, 'NOT_RUN: no timed captures'
    frames = [json.loads(p.read_text()) for p in files]
    assert len({j['LastSendUnixMs'] for j in frames}) == 1, 'input changed during deadline observation'
    states = ['Copied' in ''.join(c['Content'] for row in j['Cells'] for c in row) for j in frames]
    assert states[0] and not states[-1], 'must capture both visible and expired states'
    expired = states.index(False)
    assert not any(states[expired:]), 'copy success display reappeared without input'
    elapsed = [j['CapturedUnixMs'] - j['LastSendUnixMs'] for j in frames]
    assert elapsed == sorted(elapsed), 'capture clock moved backwards'
    before, after = elapsed[expired - 1], elapsed[expired]
    assert before <= 2000 <= after, 'observed transition does not contain the SPEC two-second deadline'
    positive, negative = frames[0]['Cells'], frames[-1]['Cells']
    for j, visible in zip(frames, states):
        assert j['Cells'] == (positive if visible else negative), 'copy display changed outside its expiry'
    references[who] = (positive, negative)
    result[who] = {'last_visible_ms': before, 'first_expired_ms': after, 'captures': len(frames)}
assert references['herdr'] == references['ikesu'], 'visible or expired states differ between products'
(root / 'copy-deadline-gate.json').write_text(json.dumps(result, indent=2) + '\n')
print('PASS: identical visible/expired cells; both observed expiry intervals contain 2000 ms', result)
