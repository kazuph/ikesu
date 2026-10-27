"""Verify all five tab identities, including tabs outside the visible strip."""
import json
from pathlib import Path
import sys
root = Path(sys.argv[1])
for role in ('herdr', 'ikesu'):
    saved = json.loads((root/role/'runtime-json.json').read_text())
    rows = json.loads(saved['home/parity-api-actions.json'])
    assert len(rows) == 2
    before, after = [r['response']['result']['tabs'] for r in rows]
    assert [t['label'] for t in before] == [letter*8 for letter in 'ABCDE'], before
    assert [t['label'] for t in after] == [letter*8 for letter in 'BACDE'], after
    assert [t['tab_id'] for t in after] == [before[i]['tab_id'] for i in (1, 0, 2, 3, 4)], after
    assert [t['tab_id'] for t in before if t['focused']] == [t['tab_id'] for t in after if t['focused']], 'active tab changed'
print('PASS: five overflowing tab identities, ordering and active identity match')
