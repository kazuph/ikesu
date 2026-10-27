#!/usr/bin/env python3
"""Keep raw screen diffs and a strict observable-state summary beside captures."""
import argparse
import difflib
import json
from pathlib import Path
p = argparse.ArgumentParser()
p.add_argument('directory')
a = p.parse_args()
root = Path(a.directory)
rows = []
for h in sorted((root / 'herdr').glob('*.txt')):
    if h.name == 'server-pid.txt':
        continue
    g = root / 'ikesu' / h.name
    if not g.exists():
        rows.append({'screen': h.name, 'status': 'NOT_RUN', 'reason': 'ikesu capture absent'})
        continue
    ht, gt = h.read_text(), g.read_text()
    diff = ''.join(difflib.unified_diff(ht.splitlines(True), gt.splitlines(True), fromfile=str(h), tofile=str(g)))
    (root / (h.stem + '.diff')).write_text(diff)
    # These are observed strings, not substitute values for unsupported operations.
    tokens = ['Split vertical', 'Rename pane', 'Rename tab', 'settings', 'keybinds',
              'PARITY_OUTSIDE', 'ROUTE_TARGET', 'CHROME_DONE', 'COPY', 'PREFIX']
    rows.append({'screen': h.name, 'text_equal': ht == gt,
                 'herdr': {t: ht.count(t) for t in tokens},
                 'ikesu': {t: gt.count(t) for t in tokens}})
(root / 'comparison.json').write_text(json.dumps(rows, ensure_ascii=False, indent=2)+'\n')
print(json.dumps(rows, ensure_ascii=False, indent=2))
