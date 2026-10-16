#!/usr/bin/env python3
"""A21: navigation on first click; log on second; missing targets are inert."""
import json
import re
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'gtx'):
    results = {}
    for name in ('before-click', 'first-click', 'second-click', 'missing-log', 'missing-owner'):
        text = (root / product / (name + '.txt')).read_text()
        pane = re.search(r'┌ %([0-9]+) ', text).group(1)
        results[name] = {'pane': pane, 'popup': 'job log:' in text}
    assert results['before-click']['pane'] != results['first-click']['pane'], results
    assert not results['first-click']['popup'] and results['second-click']['popup'], results
    assert results['first-click']['pane'] == results['second-click']['pane'], results
    assert not results['missing-log']['popup'] and not results['missing-owner']['popup'], results
    assert results['missing-owner']['pane'] == results['before-click']['pane'], results
    proof[product] = results
proof['A21'] = 'PASS'
print(json.dumps(proof, indent=2))
