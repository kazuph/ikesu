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
burst = re.search(r'wheelburst count=(\d+) rate=(\d+) times_unix_us=(\[[^\n]*\])', (root/'viewer.log').read_text())
if burst is None:
    raise SystemExit('missing timed scroll input record')
scroll_count, nominal_rate = int(burst[1]), int(burst[2])
scroll_times = json.loads(burst[3])
if len(scroll_times) != scroll_count or scroll_count < 2:
    raise SystemExit('incomplete timed scroll record')
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
    'image_upload_commands': uploads, 'scroll_events': scroll_count, 'nominal_scroll_hz': nominal_rate,
    'scroll_to_click_ms': scroll_duration,
    'observed_average_scroll_hz': (scroll_count-1)*1000000/(scroll_times[-1]-scroll_times[0]),
    'scroll_times_unix_us': scroll_times,
    'mini_load': (root/'mini-load.log').read_text().strip(),
    'events': events,
}
if inputs.get('tap_tb_input'):
    tapped = (root/'remote-tb-input.bin').read_bytes()
    # Mouse M/m terminators must not count as misdelivered typing.
    typed = bytearray()
    unknown = []
    cursor = 0
    while cursor < len(tapped):
        if tapped[cursor] != 27:
            typed.append(tapped[cursor])
            cursor += 1
            continue
        control = re.match(rb'\x1b([_P\]])(.*?)(?:\x1b\\|\x07)', tapped[cursor:], re.S)
        if control:
            # Kitty replies, palette replies and terminal capability replies
            # are complete control strings, not text typed into the old pane.
            if ((control[1] == b'_' and control[2].startswith(b'G'))
                    or (control[1] == b']' and re.match(rb'(?:4|10|11|12);', control[2]))
                    or (control[1] == b'P' and re.match(rb'(?:[01]\+r|[01]\$r)', control[2]))):
                cursor += len(control[0])
                continue
            unknown.append(control[0].hex())
            cursor += len(control[0])
            continue
        match = re.match(rb'\x1b\[[0-?]*[ -/]*[@-~]', tapped[cursor:])
        if match is None:
            unknown.append(tapped[cursor:].hex())
            break
        sequence = match[0]
        if re.fullmatch(rb'\x1b\[<\d+;\d+;\d+[Mm]', sequence):
            pass
        elif sequence in (b'\x1b[I', b'\x1b[O') or re.fullmatch(rb'\x1b\[\?\d+(?:;\d+)?(?:\$y|u)', sequence):
            pass
        elif sequence in (b'\x1b[200~', b'\x1b[201~'):
            pass
        elif key := re.fullmatch(rb'\x1b\[(\d+)(?:;\d+)?u', sequence):
            typed.extend(chr(int(key[1])).encode())
        elif not re.fullmatch(rb'\x1b\[[\d;?]*[Rct]', sequence):
            unknown.append(sequence.hex())
        cursor += len(sequence)
    result.update(old_pane_raw_input_hex=tapped.hex(), old_pane_typed_input_hex=typed.hex(),
                  old_pane_unclassified_sequences=unknown,
                  old_pane_misdelivered_key_bytes=None if unknown else sum(Counter(typed)[b] for b in set(expected)))
(root/'measurement.json').write_text(json.dumps(result, indent=2)+'\n')
print(json.dumps(result))
