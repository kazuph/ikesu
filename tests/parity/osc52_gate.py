#!/usr/bin/env python3
"""Compare captured copy bytes and require the approved pasteboard sandbox proof."""
import base64
import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

from clipboard_guard import SANDBOX_PROFILE, require_no_copy_write

p = argparse.ArgumentParser()
p.add_argument('directory', type=Path)
p.add_argument('--expect-empty', action='store_true', help='require an unselected exit to emit no copy')
a = p.parse_args()
root = a.directory
guard = root / 'clipboard-guard'
probe = json.loads((guard / 'sandbox-probe.json').read_text())
assert probe['exit_code'] == 1, 'STOP: child pbcopy was not denied'
proof = json.loads((guard / 'proof.json').read_text())
assert [row['process'] for row in proof] == ['herdr', 'ikesu'], 'both products must finish'
require_no_copy_write([dict(probe, copy_candidate_sha256=probe.get('copy_candidate_sha256', [])), *proof])
decoded = {}
for who in ('herdr', 'ikesu'):
    case = root / who
    launch = json.loads((case / 'sandbox-launch.json').read_text())
    assert launch['launcher'][:3] == ['/usr/bin/sandbox-exec', '-p', SANDBOX_PROFILE], who
    if launch['server']:
        cleanup = json.loads((case / 'cleanup.json').read_text())
        assert cleanup['exit'] == 0 and any('"type":"ok"' in reply for reply in cleanup['replies']), cleanup
    parts = re.findall(rb'\x1b\]52;[^;]*;([^\x07\x1b]*)(?:\x07|\x1b\\)', (case / 'terminal.raw').read_bytes())
    decoded[who] = [base64.b64decode(part, validate=True) for part in parts]
if a.expect_empty:
    assert not decoded['herdr'], 'unselected exit copied data'
else:
    assert decoded['herdr'], 'NOT_MET: no captured OSC52 copy'
assert decoded['herdr'] == decoded['ikesu'], 'copied bytes or copy count differ'
result = {'status': 'PASS', 'equal': True, 'count': len(decoded['herdr']),
          'copies': [{'bytes': len(value), 'sha256': hashlib.sha256(value).hexdigest()}
                     for value in decoded['herdr']]}
(root / 'osc52-gate.json').write_text(json.dumps(result, indent=2) + '\n')
print('PASS: sandbox, no clipboard hash matching copied bytes and exact OSC52 bytes', result['count'])
