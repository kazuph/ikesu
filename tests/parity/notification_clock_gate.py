#!/usr/bin/env python3
"""Compare real toast timing within the renderer's existing tick granularity."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
tick_source = Path(__file__).resolve().parents[2]/'internal/tui/tui.go'
match = re.search(r'tickInterval\s*=\s*(\d+)\s*\*\s*time.Millisecond', tick_source.read_text())
assert match, 'existing render tick interval not found'
tick_ms = int(match[1])
proof = {'render_tick_ms':tick_ms, 'source':str(tick_source), 'measurements':{}}
for who in ('herdr', 'ikesu'):
    case = root / who
    before = json.loads((case/'notification-visible.json').read_text())
    ended = json.loads((case/'expiry-ten.json').read_text())
    notification = json.loads((case/'notification-visible.json').read_text())
    toast_rows = [(y, ''.join(c['Content'] for c in row)) for y,row in enumerate(before['Cells'])
                  if '● 2 other' in ''.join(c['Content'] for c in row) and ''.join(c['Content'] for c in row).index('● 2 other') > ''.join(c['Content'] for c in row).index('│')]
    assert len(toast_rows) == 1
    ty, text = toast_rows[0]
    tx = text.index('● 2 other')
    state_rows = [(y,row) for y,row in enumerate(notification['Cells'])
                  if ''.join(c['Content'] for c in row).split('│',1)[0].strip() == '● 2 other']
    assert len(state_rows) == 1
    sy, row = state_rows[0]
    sx = next(x for x,c in enumerate(row) if c['Content'] == '●')
    assert ended['Cells'][ty][tx]['Content'] != '●'
    event = notification['CellChangedUnixMs'][sy][sx]
    shown = before['CellChangedUnixMs'][ty][tx]
    hidden = ended['CellChangedUnixMs'][ty][tx]
    assert event < shown < hidden
    proof['measurements'][who] = {'state_changed_unix_ms':event,'shown_unix_ms':shown,
                                 'hidden_unix_ms':hidden,'delay_ms':shown-event,'visible_ms':hidden-shown}
    cleanup = json.loads((case/'cleanup.json').read_text())
    assert cleanup['exit'] == 0
h, g = [proof['measurements'][who] for who in ('herdr','ikesu')]
for field in ('delay_ms','visible_ms'):
    proof[field+'_difference'] = abs(h[field]-g[field])
(root/'notification-clock-gate.json').write_text(json.dumps(proof,ensure_ascii=False,indent=2)+'\n')
for field in ('delay_ms','visible_ms'):
    assert abs(h[field]-g[field]) <= tick_ms, (field,h[field],g[field],tick_ms)
print('real notification start/expiry timing within existing render tick: PASS', proof['measurements'])
