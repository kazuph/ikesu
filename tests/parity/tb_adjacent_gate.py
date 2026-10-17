#!/usr/bin/env python3
"""Check immediate adjacent-pane typing and actual old-browser PTY input."""
from collections import Counter
import json
from pathlib import Path
import sys

from tb_input_records import typed_input

root = Path(sys.argv[1])
parameters = json.loads((root/'inputs.json').read_text())
if not parameters.get('adjacent_input') or not parameters['tap_tb_input']:
    raise SystemExit('requires an actual adjacent-pane observation and browser PTY tap')
expected = parameters['expected_keys'].encode()
received = bytes.fromhex(json.loads((root/'adjacent-keys.json').read_text())['received_hex'])
actual = received.rstrip(b'\r\n')
tapped = (root/'remote-tb-input.bin').read_bytes()
old, unknown = typed_input(tapped)
report = {
    'scope': 'one real saved-machine adjacent-pane click; no timing or native-display acceptance',
    'procedure_complete': 'TIMEOUT' not in (root/'viewer.log').read_text(),
    'input_received_hex': received.hex(),
    'missing_key_bytes': sum((Counter(expected)-Counter(actual)).values()),
    'extra_key_bytes': sum((Counter(actual)-Counter(expected)).values()),
    'input_order_exact': received == expected+b'\r',
    'old_pane_typed_hex': old.hex(),
    'old_pane_unclassified_sequences': unknown,
    'old_pane_misdelivered_key_bytes': None if unknown else sum(Counter(old)[b] for b in set(expected)),
    'mini_load': (root/'mini-load.log').read_text().strip(),
}
passed = (report['procedure_complete'] and report['input_order_exact']
          and report['missing_key_bytes'] == report['extra_key_bytes'] == report['old_pane_misdelivered_key_bytes'] == 0)
report['passed'] = passed
(root/'adjacent-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if passed else 1)
