#!/usr/bin/env python3
"""Compare measured page navigation, popup borders and delivered Escape bytes."""
import json
from pathlib import Path
import re
import sys

root = Path(sys.argv[1])
results = []
for mode in ['local', 'server']:
    for case in ['page-plain', 'page-cursor', 'page-alternate']:
        measured = {}
        for name in ['herdr', 'gtx']:
            folder = root/mode/case/name
            measured[name] = {screen: re.findall(r'PROBE_LINE_\d+', (folder/(screen+'.txt')).read_text())
                              for screen in ['live', 'up', 'release', 'down', 'shift-up']}
            measured[name]['bytes'] = ''.join(json.loads(row)['hex'] for row in (folder/'received.jsonl').read_text().splitlines())
        results.append({'mode': mode, 'case': case, 'measurements': measured,
                        'pass': measured['herdr'] == measured['gtx'] and bool(measured['herdr']['live'])})
    for case in ['plain', 'alternate', 'cursor', 'focus', 'mouse', 'kitty', 'alt-escape']:
        measured = {}
        for name in ['herdr', 'gtx']:
            folder = root/mode/('popup-'+case)/name
            screen = folder/(('alt-escape' if case == 'alt-escape' else 'escape')+'.txt')
            text = screen.read_text()
            capture = folder/'received.jsonl'
            clock = json.loads(screen.with_suffix('.json').read_text())['CapturedUnixMs']
            records = [json.loads(row) for row in capture.read_text().splitlines()] if capture.exists() else []
            measured[name] = {'top_left_borders': text.count('┌'), 'top_right_borders': text.count('┐'),
                              'bytes': ''.join(row['hex'] for row in records if row['unix_ns'] // 1000000 <= clock)}
        expected_borders = 1 if case == 'plain' else 2
        results.append({'mode': mode, 'case': 'popup-'+case, 'measurements': measured,
                        'pass': measured['herdr'] == measured['gtx'] and
                        measured['herdr']['top_left_borders'] == expected_borders and
                        measured['herdr']['top_right_borders'] == expected_borders})
(root/'navigation-gate.json').write_text(json.dumps(results, indent=2)+'\n')
for row in results:
    print(row['mode'], row['case'], 'PASS' if row['pass'] else 'FAIL')
raise SystemExit(0 if all(row['pass'] for row in results) else 1)
