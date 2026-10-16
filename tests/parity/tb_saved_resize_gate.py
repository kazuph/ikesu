#!/usr/bin/env python3
"""Inspect actual saved-tb output while real SSH bytes are held at resize."""
import base64
import hashlib
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
parameters = json.loads((root/'inputs.json').read_text())
if not parameters.get('resize_wait') or not parameters['capture_ssh']:
    raise SystemExit('requires an actual saved-machine resize/SSH hold observation')
log = (root/'viewer.log').read_text()
raw = (root/'terminal.raw').read_bytes()
begin = int(re.search(r'event mark label="resize-start" at_unix_ms=\d+ outer_bytes=(\d+)', log)[1])
end = int(re.search(r'stats pending-resize .*?outer_bytes_so_far=(\d+)', log)[1])
if not 0 <= begin <= end <= len(raw):
    raise SystemExit('invalid pending display boundaries')
held = json.loads((root/'ssh/transport-held.json').read_text())
resumed = json.loads((root/'ssh/transport-resumed.json').read_text())
pending = json.loads((root/'pending-resize.json').read_text())
images, chunks, placements = {}, {}, {}
chunk_ident = None
states = []
start = 0
file_records = iter(json.loads(line) for line in (root/'file-images.jsonl').read_text().splitlines()) if (root/'file-images.jsonl').exists() else iter(())
for stop in (begin, end):
    for match in re.finditer(rb'\x1b_G(.*?)\x1b\\', raw[start:stop], re.S):
        header, _, payload = match[1].partition(b';')
        keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
        action = keys.get(b'a', b't')
        ident = int(keys.get(b'i', str(chunk_ident or 0).encode()))
        if action == b't':
            if keys.get(b't', b'd') == b'f':
                record = next(file_records, None)
                if (not record or 'error' in record or record['control'] != header.decode()
                        or record['path'].encode() != base64.b64decode(payload, validate=True)
                        or record['raw_bytes_end'] > stop
                        or keys.get(b'f') != b'32'
                        or record['length'] != int(keys[b's'])*int(keys[b'v'])*4):
                    raise SystemExit('actual file pixels were not captured at the matching upload')
                images[ident] = record['sha256']
                continue
            if keys.get(b't', b'd') != b'd':
                raise SystemExit('unsupported non-inline outer image; no payload retention verdict')
            chunks[ident] = chunks.get(ident, b'')+base64.b64decode(payload, validate=True)
            if keys.get(b'm', b'0') == b'0':
                images[ident] = hashlib.sha256(chunks.pop(ident)).hexdigest()
                chunk_ident = None
            else:
                chunk_ident = ident
        elif action == b'p':
            placements[ident, int(keys.get(b'p', b'0'))] = ident
        elif action == b'd':
            mode = keys.get(b'd', b'a')
            if mode in (b'a', b'A'):
                placements.clear()
            elif mode in (b'i', b'I'):
                pid = int(keys.get(b'p', b'0'))
                placements = {key: value for key, value in placements.items()
                              if key[0] != ident or pid and key[1] != pid}
            else:
                raise SystemExit('unsupported outer image deletion mode')
    states.append({str(ident): images[ident] for ident in set(placements.values())})
    start = stop
period = raw[begin:end]
counts = {'screen_clears': period.count(b'\x1b[2J'), 'uploads': period.count(b'a=t,'),
          'deletes': period.count(b'a=d,'), 'all_graphics_controls': period.count(b'\x1b_G')}
clock_valid = (held['pid'] == resumed['pid'] and held['bytes'] == resumed['bytes'] > 0
               and held['time_unix_ns']//1_000_000 <= pending['CapturedUnixMs'] <= resumed['time_unix_ns']//1_000_000)
passed = (bool(states[0]) and states[0] == states[1] and not any(counts.values())
          and clock_valid and 'TIMEOUT' not in log and (pending['Cols'], pending['Rows']) == (140, 40))
report = {'scope': 'real SSH bytes withheld across saved-tb resize; image payload/placement retention only; native missing frames unverified',
          'counts_during_pending_resize': counts, 'image_payload_sha256_before_and_pending': states,
          'actual_output_held': held, 'actual_output_resumed': resumed, 'pending_capture_unix_ms': pending['CapturedUnixMs'],
          'hold_clock_valid': clock_valid, 'passed': passed}
(root/'resize-wait-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
raise SystemExit(0 if passed else 1)
