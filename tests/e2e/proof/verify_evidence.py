"""Check saved PTY screens against the actual pending/resolved records."""
import json, os, pathlib
p = pathlib.Path(os.environ['NODIALOG_EVIDENCE_DIR'])
checks = 0
for choice in ['approve', 'reject']:
    d = json.loads((p / f'guard-{choice}-pending.json').read_text())
    screen = (p / f'guard-{choice}-pending.screen.txt').read_text()
    assert d['kind'] == 'command_guard' and d['status'] == 'pending'
    assert all(text not in screen for text in [d['title'], '! 1', 'esc close', '1日許可'])
    assert 'PENDING_INPUT' in screen
    checks += 3
for name, dialog in [('new-client-old-server', False), ('old-client-new-server', True)]:
    d = json.loads((p / f'{name}-result.json').read_text())
    assert d['pending']['status'] == 'pending' and d['resolved']['status'] == 'answered'
    assert ('MIX_PENDING_TITLE' in (p / f'{name}-pending.screen.txt').read_text()) == dialog
    assert 'MIX_ANSWERED' in (p / f'{name}-after.screen.txt').read_text()
    checks += 3
for name, dialog in [('saved-new-client-old-server', False), ('saved-old-client-new-server', True)]:
    d = json.loads((p / name / 'saved-result.json').read_text())
    lines = [json.loads(line) for line in (p / name / 'saved-watch.jsonl').read_text().splitlines()]
    assert d['resolved']['status'] == 'answered'
    assert ('SAVED_PENDING_TITLE' in (p / name / 'saved-pending.screen.txt').read_text()) == dialog
    assert 'REMOTE_INPUT' in (p / name / 'saved-remote-pane.json').read_text()
    assert 'decision_already_resolved' in d['duplicate']
    for event in ['created', 'resolved']:
        assert any(row['event'] == event and row['machine']['label'] == 'RemoteProof' and row.get('decision', {}).get('decision_id') == d['pending']['decision_id'] for row in lines)
    checks += 6
print(json.dumps({'status': 'PASS', 'checks': checks}))
