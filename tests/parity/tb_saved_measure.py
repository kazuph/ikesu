#!/usr/bin/env python3
"""Derive saved-tb measurements from actual input, PTY bytes, and cell times."""
import argparse
from collections import Counter
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()
root = a.out
inputs = json.loads((root/'inputs.json').read_text())
if not inputs.get('measure'):
    raise SystemExit('not a measurement run')
events = []
for m in re.finditer(r'event (\S+) label=("(?:\\.|[^"])*") at_unix_ms=(\d+) outer_bytes=(\d+)',
                     (root/'viewer.log').read_text()):
    events.append({'kind': m[1], 'label': json.loads(m[2]), 'time_ms': int(m[3]), 'bytes': int(m[4])})

def event(kind, label):
    matches = [e for e in events if e['kind'] == kind and e['label'] == label]
    if len(matches) != 1:
        raise SystemExit(f'expected one {kind} {label}, got {matches}')
    return matches[0]

start, click, end = event('mark', 'scroll-start'), event('clicklocated', 'TB_LOCAL'), event('mark', 'measurement-end')
raw = (root/'terminal.raw').read_bytes()
if not 0 <= start['bytes'] <= click['bytes'] <= end['bytes'] <= len(raw):
    raise SystemExit('invalid output byte boundaries')
period = raw[start['bytes']:end['bytes']]
apcs = list(re.finditer(rb'\x1b_G(.*?)\x1b\\', period, re.S))
uploads = sum(1 for m in apcs if re.search(rb'(?:^|,)a=[tT](?:,|;)', m[1]))
screen = json.loads((root/'switch-local.json').read_text())
marker = 'LOCAL_SWITCH_READY'
marker_times = []
for y, cells in enumerate(screen['Cells']):
    text = ''.join(c['Content'] if len(c['Content']) == 1 else ' ' for c in cells)
    x = text.find(marker)
    if x >= 0:
        marker_times.append(max(screen['CellChangedUnixMs'][y][x:x+len(marker)]))
if len(marker_times) != 1 or not click['time_ms'] <= marker_times[0] <= screen['CapturedUnixMs']:
    raise SystemExit(f'missing or stale switch marker: {marker_times}, click={click}')
received = bytes.fromhex(json.loads((root/'keys.json').read_text())['received_hex'])
expected = inputs['expected_keys'].encode()
actual = received.rstrip(b'\r\n')
missing = sum((Counter(expected)-Counter(actual)).values())
duplicated = sum((Counter(actual)-Counter(expected)).values())
steps = (root/'steps').read_text()
scroll_count = steps.count('mouse wheel-down 80 22\n')
scroll_duration = click['time_ms']-start['time_ms']
result = {
    'scope': 'one diagnostic run; no native-terminal missing-frame proof or median comparison',
    'procedure_complete': 'TIMEOUT' not in (root/'viewer.log').read_text(),
    'product': inputs['product'], 'version': inputs['version'], 'binary_sha256': inputs['sha256'],
    'switch_display_ms': marker_times[0]-click['time_ms'],
    'input_received_hex': received.hex(), 'input_expected_hex': (expected+b'\r').hex(),
    'missing_key_bytes': missing, 'extra_key_bytes': duplicated, 'input_order_exact': actual == expected,
    'screen_clear_count': period.count(b'\x1b[2J'),
    'outer_terminal_bytes': len(period), 'kitty_apc_bytes': sum(len(m[0]) for m in apcs),
    'kitty_payload_base64_bytes': sum(len(m[1].partition(b';')[2]) for m in apcs),
    'image_upload_commands': uploads, 'scroll_events': scroll_count, 'nominal_scroll_hz': 125,
    'scroll_to_click_ms': scroll_duration,
    'observed_average_scroll_hz': scroll_count*1000/scroll_duration if scroll_duration else None,
    'mini_load': (root/'mini-load.log').read_text().strip(),
    'events': events,
}
(root/'measurement.json').write_text(json.dumps(result, indent=2)+'\n')
print(json.dumps(result))
