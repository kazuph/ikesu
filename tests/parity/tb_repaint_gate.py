#!/usr/bin/env python3
"""Check image placement continuity across actual saved-tb UI inputs."""
import json
from pathlib import Path
import re
import sys

report = {'scope': 'nonempty terminal placements; native missing frames unverified', 'products': {}}
for root in map(Path, sys.argv[1:]):
    inputs = json.loads((root/'inputs.json').read_text())
    assert inputs['repaint_probe'] and not inputs['measure']
    if inputs['product'] == 'herdr':
        assert inputs['version'] == 'herdr 0.2.57'
    raw, log = (root/'terminal.raw').read_bytes(), (root/'viewer.log').read_text()
    assert 'TIMEOUT' not in log
    phases = {}
    for phase in ('same-space', 'sidebar-scroll', 'focus-return'):
        start = int(re.search('event mark label="'+phase+r'-start" at_unix_ms=\d+ outer_bytes=(\d+)', log)[1])
        end = int(re.search('stats '+phase+r'-end .*?outer_bytes_so_far=(\d+)', log)[1])
        assert 0 <= start <= end <= len(raw)
        assert (root/(phase+'.json')).exists()
        placements, empty, before = set(), 0, None
        for control in re.finditer(rb'\x1b_G(.*?)\x1b\\', raw[:end], re.S):
            if before is None and control.start() >= start:
                before = sorted(placements)
            header = control[1].partition(b';')[0]
            keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
            action = keys.get(b'a', b't')
            ident, placement = int(keys.get(b'i', b'0')), int(keys.get(b'p', b'0'))
            if action == b'p':
                placements.add((ident, placement))
            elif action == b'd':
                mode = keys.get(b'd', b'a')
                if mode in (b'a', b'A'):
                    placements.clear()
                elif mode in (b'i', b'I'):
                    placements = {pair for pair in placements if pair[0] != ident or placement and pair[1] != placement}
                else:
                    raise AssertionError(('unhandled deletion', header))
            if control.start() >= start and not placements:
                empty += 1
        if before is None:
            before = sorted(placements)
        chunk = raw[start:end]
        phases[phase] = {'screen_clears': chunk.count(b'\x1b[2J'), 'deletes': chunk.count(b'a=d,'),
                         'controls': chunk.count(b'\x1b_G'), 'placements_before': before,
                         'placements_after': sorted(placements), 'empty_placement_steps': empty}
        phases[phase]['passed'] = bool(before and placements and empty == 0 and b'\x1b[2J' not in chunk)
    cleanup = json.loads((root/'cleanup.json').read_text())
    assert len(cleanup) == 2 and all(row['reply']['result']['type'] == 'ok' for row in cleanup)
    report['products'][inputs['product']] = phases
assert set(report['products']) == {'herdr', 'ikesu'}
report['passed'] = all(phase['passed'] for phases in report['products'].values() for phase in phases.values())
(Path(sys.argv[-1])/'repaint-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if report['passed'] else 1)
