#!/usr/bin/env python3
"""Check saved real PTY input fields, sidebar feedback, and OSC title bytes."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
rows = []
def check(condition, mode, product, evidence, predicate):
    path = root / evidence
    try:
        data = path.read_bytes()
        value = predicate(data)
        rows.append(dict(condition=condition, mode=mode, product=product,
                         evidence=evidence, status='PASS' if value else 'FAIL'))
    except (OSError, ValueError) as error:
        rows.append(dict(condition=condition, mode=mode, product=product,
                         evidence=evidence, status='NOT_RUN', error=str(error)))

for mode in ('standalone', 'server'):
    for product in ('herdr', 'ikesu'):
        prefix = f'observe-{mode}/{product}/'
        check('rename bracketed paste', mode, product, prefix+'name-paste.txt',
              lambda b: 'new tab' in b.decode() and '日本語 alpha' in b.decode())
        check('navigator bracketed paste', mode, product, prefix+'navigator-paste.txt',
              lambda b: ' / 日本語 alpha' in b.decode() and 'new tab' not in b.decode())
        check('copy search strips control characters', mode, product, prefix+'search-paste.txt',
              lambda b: '/alphabeta' in b.decode())
        check('three-line sidebar feedback', mode, product, prefix+'copied-three.txt',
              lambda b: b.decode().splitlines()[-1].startswith('Copied 3 lines'))
        # The reference legacy UI uses the plural even for one line. Record it
        # explicitly; do not relabel this as identical output.
        one = 'Copied 1 lines' if product == 'herdr' and mode == 'standalone' else 'Copied 1 line'
        check('one-line sidebar feedback: '+one, mode, product, prefix+'copied-one.txt',
              lambda b, text=one: b.decode().splitlines()[-1].startswith(text))
        check('feedback expires', mode, product, prefix+'expired.txt',
              lambda b: 'Copied' not in b.decode())
        name = product+(':parity' if mode == 'server' else '')
        def titles(data, name=name, product=product):
            values = re.findall(rb'\x1b\]0;([^\x07\x1b]*)(?:\x07|\x1b\\)', data)
            return len(values) >= 2 and values[0].decode() == name and values[-1].decode() == product
        check('plain base name and reset on exit/detach', mode, product,
              f'title-{mode}/{product}/terminal.raw', titles)

(root/'condition-gate.json').write_text(json.dumps(rows, ensure_ascii=False, indent=2)+'\n')
for row in rows:
    print(row['status'], row['mode'], row['product'], row['condition'])
raise SystemExit(any(row['status'] != 'PASS' for row in rows))
