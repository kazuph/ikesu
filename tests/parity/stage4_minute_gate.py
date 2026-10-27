#!/usr/bin/env python3
"""A25: a real, still-running job crosses a minute; no timestamp rewriting."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
proof = {}
for product in ('herdr', 'ikesu'):
    folder = root / product
    saved = json.loads((folder / 'runtime-json.json').read_text())
    log = json.loads(saved['home/parity-job-status.json'])
    assert log['exit'] == 0, (product, log)
    assert "'/bin/sleep' '120'" in log['stdout'], (product, log)
    assert 'finished_unix_ms:' not in log['stdout'], (product, log)
    started = int(re.search(r'^started_unix_ms: (\d+)$', log['stdout'], re.M).group(1))
    checked = json.loads(saved['home/stage4-minute-clock.json'])['unix_ms']
    minute_ms = 60 * 1000
    assert minute_ms <= checked - started < 2 * minute_ms, (product, started, checked)
    row = (folder / 'minute.txt').read_text().splitlines()[26][:25]
    assert row.startswith(' run ') and row[13:17].strip() == '1m', (product, row)
    cancelled = json.loads(saved['home/parity-job-cancel.json'])
    assert cancelled['exit'] == 0, (product, cancelled)
    processes = json.loads(saved['home/parity-job-processes.json'])
    assert processes['group_alive'] is False, (product, processes)
    assert json.loads((folder / 'cleanup.json').read_text())['exit'] == 0
    proof[product] = {'actual_elapsed_ms': checked - started, 'displayed_age': '1m',
                      'runner_group_gone_after_cancel': True}
proof['A25_running_minute'] = 'PASS'
print(json.dumps(proof, indent=2))
