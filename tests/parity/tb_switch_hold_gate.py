#!/usr/bin/env python3
"""Record real saved-machine input expiry/disconnect without inventing reference outcomes."""
from collections import Counter
import json
from pathlib import Path
import re
import sys

from tb_input_records import typed_input

root = Path(sys.argv[1])
parameters = json.loads((root/'inputs.json').read_text())
mode = parameters.get('switch_hold')
if mode not in ('expiry', 'disconnect') or not parameters['capture_ssh'] or not parameters['tap_tb_input']:
    raise SystemExit('requires an actual switch hold, SSH capture and browser input tap')
expected = parameters['expected_keys'].encode()
old = bytes.fromhex(json.loads((root/'keys.json').read_text())['received_hex'])
new, unknown = typed_input((root/'remote-tb-input.bin').read_bytes())
held = json.loads((root/'ssh/transport-held.json').read_text())
control = json.loads((root/'switch-hold-control.json').read_text())
pending = json.loads((root/'pending-input.json').read_text())
post = json.loads((root/'post-hold.json').read_text())
log = (root/'viewer.log').read_text()
clicked = int(re.search(r'event clicklocated label="TB_REMOTE" at_unix_ms=(\d+)', log)[1])
finished = json.loads((root/('ssh/transport-resumed.json' if mode == 'expiry' else 'ssh/transport-disconnected.json')).read_text())
display_connections = []
for file in (root/'ssh').glob('*-from-mini.bin'):
    first = file.read_bytes().partition(b'\n')[0]
    if first == b'herdr-saved-ssh-bridge-ready':
        display_connections.append(file.name)
        continue
    try:
        welcome = json.loads(first)
    except (ValueError, UnicodeDecodeError):
        continue
    if welcome.get('type') == 'welcome':
        display_connections.append(file.name)
clock_valid = (held['bytes'] > 0 and held['pid'] == finished['pid']
               and clicked <= pending['CapturedUnixMs'] <= control['time_unix_ns']//1_000_000 <= post['CapturedUnixMs']
               and control['time_unix_ns']/1_000_000-pending['CapturedUnixMs'] >= 3000)
if mode == 'expiry':
    clock_valid = clock_valid and finished['time_unix_ns']/1_000_000-clicked >= 3000
else:
    clock_valid = clock_valid and held['time_unix_ns'] <= finished['time_unix_ns'] <= control['time_unix_ns']
actual = new.rstrip(b'\r\n')
report = {
    'scope': 'one actual saved-tb failure-side observation; no reference equivalence or native-display verdict',
    'mode': mode,
    'procedure_complete': 'TIMEOUT' not in log,
    'actual_condition_clock_valid': clock_valid,
    'destination_display_connections': display_connections,
    'old_pane_received_hex': old.hex(),
    'old_pane_misdelivered_key_bytes': sum(Counter(old)[b] for b in set(expected)),
    'destination_typed_hex': new.hex(),
    'destination_missing_key_bytes': None if unknown else sum((Counter(expected)-Counter(actual)).values()),
    'destination_extra_key_bytes': None if unknown else sum((Counter(actual)-Counter(expected)).values()),
    'destination_order_exact': None if unknown else new == expected+b'\r',
    'destination_unclassified_sequences': unknown,
    'mini_load': (root/'mini-load.log').read_text().strip(),
}
report['no_old_pane_delivery_verified'] = (report['procedure_complete'] and clock_valid
                                           and report['old_pane_misdelivered_key_bytes'] == 0 and not unknown)
report['expired_keys_dropped'] = None if mode != 'expiry' or unknown else new == b''
(root/'switch-hold-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if report['no_old_pane_delivery_verified'] else 1)
