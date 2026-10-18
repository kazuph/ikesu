"""Compare public display and API behavior against the saved real reference."""
import argparse
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('reference', type=Path)
p.add_argument('candidate', type=Path)
p.add_argument('--band-reference', type=Path, required=True)
a = p.parse_args()
cases = ['api-position-160', 'api-default-160', 'api-default-60',
         'agent-position-160', 'agent-default-160', 'agent-default-60']


def measure(root, case):
    path = root/case
    exit_info = json.loads((path/'exit.json').read_text())
    assert exit_info == {'displays': [0, 0], 'server': 0}, exit_info
    replies = json.loads((path/'api.json').read_text())
    reasons = [r['reply']['result']['reason'] for r in replies
               if r['method'] == 'notification.show']
    shots = {}
    for phase in ['visible', 'clicked', 'retried', 'expired']:
        for n in [1, 2]:
            rows = (path/f'view{n}-{phase}.txt').read_text().splitlines()
            assert len(rows) == 45, (case, phase, len(rows))
            marker = 'BODY' if case.startswith('api') else '● 2 background'
            # The toast occupies the top four rows or the mobile bottom band.
            shots[f'{n}-{phase}'] = [i for i in list(range(4))+[44]
                                      if marker in rows[i]]
    return {'reasons': reasons, 'shots': shots}


results = []
for case in cases:
    ref = a.band_reference if case == 'agent-default-60' else a.reference
    expected, actual = measure(ref, case), measure(a.candidate, case)
    results.append({'case': case, 'reference': expected, 'candidate': actual,
                    'pass': expected == actual})
(a.candidate/'two-display-gate.json').write_text(json.dumps(results, indent=2)+'\n')
for result in results:
    print(result['case'], 'PASS' if result['pass'] else 'FAIL', flush=True)
assert all(r['pass'] for r in results), results
