#!/usr/bin/env python3
"""Require all prefix frames, retaining the exact SPEC product-name exception."""
import json
from pathlib import Path
import re
import subprocess
import sys

root, steps_root = map(Path, sys.argv[1:])
results = []
for steps in sorted(steps_root.glob('*.steps')):
    names = re.findall(r'^cells @OUT@/([^\s]+)\.json$', steps.read_text(), re.M)
    assert names, steps
    for name in names:
        result = json.loads((root / steps.stem / (name+'-full-comparison.json')).read_text())
        if steps.stem == 'new-worktree' and name == 'result':
            subprocess.run([sys.executable, str(Path(__file__).with_name('worktree_warning_gate.py')),
                            str(root / steps.stem), name], check=True)
            result['status'] = 'PASS_SPEC_PRODUCT_NAME'
        assert result['status'] in ('PASS', 'PASS_SPEC_PRODUCT_NAME'), result
        results.append({k: v for k, v in result.items() if k != 'differences'})
assert results, 'no prefix evidence'
(root / 'prefix-full-gate.json').write_text(json.dumps(results, indent=2)+'\n')
print('PASS: every prefix capture', len(results))
