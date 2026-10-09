#!/usr/bin/env python3
"""Prove prefix navigation using exact identities returned by the real server."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
proof = {}
for who in ('herdr', 'gtx'):
    case = root / who
    runtime = json.loads((case / 'runtime-json.json').read_text())
    actions = json.loads(runtime['home/parity-api-actions.json'])
    owners = json.loads(runtime['home/parity-popup-owners.json'])
    assert owners['owner'] != owners['other']
    assert all('error' not in a['response'] for a in actions)
    reports = [a for a in actions if a['method'] == 'pane.report_agent']
    assert [a['params']['state'] for a in reports] in (
        ['idle', 'working', 'idle'], ['idle', 'working', 'blocked'])
    pane_lists = [a for a in actions if a['method'] == 'pane.list']
    assert pane_lists
    panes = pane_lists[-1]['response']['result']['panes']
    assert len(panes) == 1
    target = panes[0]['pane_id']
    assert all(a['params']['pane_id'] == target for a in reports[1:])
    returned = [a for a in actions if a['method'] == 'pane.get'][-1]
    viewed = returned['response']['result']['pane']
    assert viewed['pane_id'] == target and viewed['workspace_id'] == owners['other']
    assert 'NOTIFICATION_INPUT' in (case / 'input.txt').read_text()
    cleanup = json.loads((case / 'cleanup.json').read_text())
    assert cleanup['exit'] == 0
    proof[who] = {'owner': owners['owner'], 'target_workspace': owners['other'],
                  'target_pane': target, 'viewed': viewed, 'cleanup': cleanup}
(root / 'notification-target-gate.json').write_text(json.dumps(proof, ensure_ascii=False, indent=2)+'\n')
print('real notification target identity, prefix navigation, input, cleanup: PASS')
