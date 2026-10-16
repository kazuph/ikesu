#!/usr/bin/env python3
"""A17 row scrolling and A18 configured content/height from real PTY captures."""
import json
import sys
from pathlib import Path

scroll, custom = map(Path, sys.argv[1:])
proof = {}
for name in ('overflow', 'agents-down', 'agents-up', 'short', 'short-down'):
    crops = []
    for product in ('herdr', 'gtx'):
        lines = (scroll / product / (name + '.txt')).read_text().splitlines()
        start = 26 if len(lines) == 45 else 16
        crops.append([line[:25] for line in lines[start:-1]])
    assert crops[0] == crops[1], (name, crops)
    proof[name] = crops[0]
assert proof['overflow'][0].strip() == '○ overflow-10'
assert proof['agents-down'][0].strip() == 'codex'
assert proof['agents-up'] == proof['overflow']
for name, expected in (('custom-pane', 'CUSTOM_LABEL'), ('custom-workspace', 'parity')):
    for product in ('herdr', 'gtx'):
        lines = (custom / name / product / 'custom.txt').read_text().splitlines()
        assert lines[26][:25].strip() == expected, (name, product, lines[26])
        assert not lines[27][:25].strip(), (name, product, 'empty token row still visible')
    proof[name] = expected
proof['A17'] = 'PASS: one physical row per wheel, including a partial card'
proof['A18'] = 'PASS: only requested content; empty row has no visible content'
print(json.dumps(proof, ensure_ascii=False, indent=2))
