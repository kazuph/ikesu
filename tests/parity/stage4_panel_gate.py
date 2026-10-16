#!/usr/bin/env python3
"""A26: agents reset on switching; the real reference retains jobs' offset."""
import json
import re
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'gtx'):
    first = {}
    for name in ('agent-initial', 'agent-down', 'agent-return', 'job-initial', 'job-down', 'job-return'):
        line = (root / product / (name + '.txt')).read_text().splitlines()[26][:25]
        if name.startswith('job'):
            line = re.search(r'ROW_\d+', line).group(0)
        else:
            line = line.strip()
        first[name] = line
    assert first['agent-initial'] == first['agent-return'] == '○ overflow-10', first
    assert first['agent-down'] == 'codex', first
    assert first['job-initial'] == 'ROW_20', first
    assert first['job-down'] == first['job-return'] == 'ROW_19', first
    proof[product] = first
assert proof['herdr'] == proof['gtx']
proof['A26'] = 'PASS'
print(json.dumps(proof, ensure_ascii=False, indent=2))
