#!/usr/bin/env python3
"""Check form lifetime and real shell input, preserving documented text/edit differences."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
results = []
for who, title, edited in [('herdr', 'add machine: SSH target (e.g. mini)', 'offline.invaliZ'),
                           ('gtx', 'ssh target', 'offline.invalZd')]:
    case = root / who
    screens = {name: (case / (name + '.txt')).read_text() for name in
               ('open', 'typed', 'edit', 'outside', 'resized', 'escaped',
                'escape-input', 'outside-input', 'resize-escaped')}
    for name in ('open', 'typed', 'edit', 'outside', 'resized'):
        assert title in screens[name], (who, name, 'form absent')
    assert 'offline.invalid' in screens['typed'], who
    assert edited in screens['edit'], (who, 'cursor editing')
    for name in ('escaped', 'escape-input', 'outside-input', 'resize-escaped'):
        assert title not in screens[name], (who, name, 'escape did not cancel')
    for name, marker in [('escape-input', 'MACHINE_ESCAPE_INPUT'),
                         ('outside-input', 'MACHINE_OUTSIDE_INPUT')]:
        # One echoed command and one actual output; a queued input is insufficient.
        assert screens[name].count(marker) == 2, (who, name, 'shell input/output absent')
    resized = json.loads((case / 'resized.json').read_text())
    assert (resized['Cols'], resized['Rows']) == (100, 25), who
    cleanup = json.loads((case / 'cleanup.json').read_text())
    assert cleanup['exit'] == 0, (who, cleanup)
    results.append({'product': who, 'status': 'PASS_LIFETIME_AND_SHELL_INPUT',
                    'title': title, 'edited': edited, 'cleanup': cleanup})
guard = json.loads((root / 'clipboard-guard/proof.json').read_text())
assert len(guard) == 2 and all(row['unchanged'] for row in guard), guard
(root / 'machine-form-gate.json').write_text(json.dumps(results, ensure_ascii=False, indent=2) + '\n')
print('PASS: outside retains form, Escape cancels, shell input resumes; text/edit differences retained')
