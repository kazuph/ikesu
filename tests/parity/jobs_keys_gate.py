#!/usr/bin/env python3
"""Check real log-viewer input routing and return to the original shell."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
results = {}
for runtime in ('herdr', 'gtx'):
    case = root/runtime
    result = {}
    for screen in ('opened', 'q', 'typed', 'ctrl-c'):
        frame = json.loads((case/(screen+'.json')).read_text())
        lines = [''.join(c['Content'] for c in row) for row in frame['Cells']]
        titles = sum('job log: PARITY_JOB' in line for line in lines)
        assert titles == (0 if screen == 'ctrl-c' else 1), (runtime, screen, titles)
        result[screen] = titles
        if screen == 'typed':
            assert sum('qLOG_INPUT' in line for line in lines) == 1, (runtime, lines)
    assert (case/'shell.txt').read_text().count('JOB_CONTROL_C') == 2, runtime
    logs = json.loads((case/'job-logs.json').read_text())
    assert len(logs) == 1 and 'exit_code: 7\n' in next(iter(logs.values())), (runtime, logs)
    cleanup = json.loads((case/'cleanup.json').read_text())
    assert cleanup['exit'] == 0 and any('"type":"ok"' in reply for reply in cleanup['replies']), cleanup
    results[runtime] = result
(root/'keys-proof.json').write_text(json.dumps(results, indent=2)+'\n')
print('job log q/ordinary input/Ctrl-C/original shell/owned server cleanup: PASS')
