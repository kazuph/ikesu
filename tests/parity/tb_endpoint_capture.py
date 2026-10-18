#!/usr/bin/env python3
"""Inspect length-framed blobs observed on the real reference SSH transport."""
import argparse
import base64
from collections import Counter
import hashlib
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
a = p.parse_args()


def unsigned(data, offset):
    value = data[offset]
    offset += 1
    if value < 251:
        return value, offset
    width = {251: 2, 252: 4, 253: 8}[value]
    assert offset+width <= len(data)
    return int.from_bytes(data[offset:offset+width], 'little'), offset+width


transports = []
patches = []
for path in sorted((a.out/'ssh').glob('*.bin')):
    data = path.read_bytes()
    if data.startswith(b'herdr-saved-ssh-bridge-ready\n'):
        data = data.split(b'\n', 1)[1]
    elif len(data) < 4 or int.from_bytes(data[:4], 'little') > len(data)-4:
        continue
    position = 0
    counts, sizes = Counter(), Counter()
    records = []
    while position < len(data):
        assert position+4 <= len(data), 'partial frame header'
        size = int.from_bytes(data[position:position+4], 'little')
        body = data[position+4:position+4+size]
        position += 4+size
        assert len(body) == size and body, 'partial frame body'
        if body[0] != 20:
            counts['unparsed_tag_'+str(body[0])] += 1
            continue
        length, offset = unsigned(body, 1)
        name = body[offset:offset+length].decode()
        offset += length
        length, offset = unsigned(body, offset)
        payload = body[offset:offset+length]
        assert offset+length == len(body), 'unexpected blob framing'
        counts[name] += 1
        sizes[name] += length
        if name in ('endpoint.hello.v1', 'endpoint.welcome.v1'):
            records.append({'name': name, 'json': json.loads(payload)})
        elif name == 'endpoint.graphics.patch-ack.v1':
            records.append({'name': name, 'surface_revision': int(payload)})
        elif name == 'endpoint.graphics.patch.v1':
            patch = json.loads(payload)
            metadata = {key: value for key, value in patch.items() if key != 'rects'}
            metadata['rects'] = []
            for rect in patch['rects']:
                png = base64.b64decode(rect['png'], validate=True)
                metadata['rects'].append({**{key: value for key, value in rect.items() if key != 'png'},
                                          'png_bytes': len(png), 'png_sha256': hashlib.sha256(png).hexdigest()})
            metadata['wire_json_bytes'] = len(payload)
            patches.append(metadata)
    transports.append({'file': path.name, 'bytes': len(data), 'messages': dict(counts),
                       'blob_payload_bytes': dict(sizes), 'controls': records})
if not any('endpoint.hello.v1' in row['messages'] for row in transports) or not any('endpoint.welcome.v1' in row['messages'] for row in transports):
    raise SystemExit('UNSUPPORTED: capture is not the observed reference length-framed endpoint protocol')
report = {'scope': 'full captured connection, not a timed scroll-only network measurement',
          'transports': transports, 'graphics_patches': patches}
(a.out/'endpoint-wire.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps({'transports': [{'file': row['file'], 'bytes': row['bytes'], 'messages': row['messages']} for row in transports],
                  'patches': len(patches), 'patch_json_bytes': sum(row['wire_json_bytes'] for row in patches),
                  'rect_png_bytes': sum(rect['png_bytes'] for row in patches for rect in row['rects'])}))
