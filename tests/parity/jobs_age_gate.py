#!/usr/bin/env python3
"""Check real job status/age against each runtime's own timestamps and log."""
import json
from pathlib import Path
import re
import sys
root = Path(sys.argv[1])
proof = {}
for who in ('herdr', 'ikesu'):
    case = root/who
    logs = list(json.loads((case/'job-logs.json').read_text()).values())
    assert len(logs) == 1, (who, logs)
    log = logs[0]
    expected_caller = json.loads((case/'fixture.json').read_text())['result']['root_pane']['pane_id']
    started = int(re.search(r'^started_unix_ms: (\d+)$', log, re.M)[1])
    finished = int(re.search(r'^finished_unix_ms: (\d+)$', log, re.M)[1])
    exit_code = int(re.search(r'^exit_code: (\d+)$', log, re.M)[1])
    assert '[stdout] PARITY_JOB_OUTPUT\n' in log
    proof[who] = []
    for screen in ('running', 'wheel', 'log-closed', 'finished', 'finished-later', 'resize'):
        data = json.loads((case/(screen+'.json')).read_text())
        rows = [''.join(c['Content'] for c in row[:25]) for row in data['Cells']]
        matched_rows = [y for y,row in enumerate(rows) if 'PARITY_' in row]
        matches = [rows[y] for y in matched_rows]
        assert len(matches) == 1, (who, screen, matches)
        status, caller, elapsed, label = matches[0].split()
        painted = max(data['CellChangedUnixMs'][matched_rows[0]][13:17]) if 'CellChangedUnixMs' in data else data['CapturedUnixMs']
        assert started <= painted <= data['CapturedUnixMs'], (who, screen, 'invalid paint time')
        running = painted < finished
        expected_status = 'run' if running else ('ok' if exit_code == 0 else 'x'+str(exit_code))
        since = started if running else finished
        expected_age = str((painted-since)//1000)+'s'
        assert status == expected_status and elapsed == expected_age, (who, screen, matches[0], expected_age)
        assert caller == expected_caller and label == 'PARITY_', matches[0]
        proof[who].append({'screen': screen, 'status': status, 'elapsed': elapsed,
                           'captured_unix_ms': data['CapturedUnixMs'], 'painted_unix_ms':painted, 'since_unix_ms': since})
    cleanup = json.loads((case/'cleanup.json').read_text())
    assert cleanup['exit'] == 0 and any('"type":"ok"' in reply for reply in cleanup['replies']), cleanup
(root/'age-proof.json').write_text(json.dumps(proof, indent=2)+'\n')
print('job status, timestamp-derived age and owned server cleanup: PASS')
