#!/usr/bin/env python3
"""Require sound to be disabled in every actual isolated comparison config."""
import json
from pathlib import Path
import sys
import tomllib

root = Path(sys.argv[1])
proof = {}
for case in sorted(root.iterdir()):
    if not case.is_dir():
        continue
    reference = json.loads((case / 'reference.json').read_text())
    assert reference['sound_disabled'] is True, case
    checked = []
    for product in ('herdr', 'ikesu'):
        configs = json.loads((case / product / 'config-files.json').read_text())
        content = configs[f'config/{product}/config.toml']
        sound = tomllib.loads(content)['ui']['sound']
        assert sound['enabled'] is False, (case, product, sound)
        assert 'on' not in sound.get('agents', {}).values(), (case, product, sound)
        checked.append(product)
    proof[case.name] = checked
assert proof, 'no comparison configs checked'
proof['sound_disabled_in_all_actual_configs'] = 'PASS'
print(json.dumps(proof, indent=2))
