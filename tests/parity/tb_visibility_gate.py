#!/usr/bin/env python3
"""Compare graphics capability replies after actual saved-viewer focus/zoom."""
import json
from pathlib import Path
import sys

reference, current = map(Path, sys.argv[1:])
replies = []
expected = [('single', 'original', True),
            ('split-original-focused', 'original', True), ('split-original-focused', 'split', True),
            ('split-new-focused', 'original', True), ('split-new-focused', 'split', True),
            ('zoom-new', 'original', False), ('zoom-new', 'split', True),
            ('restored', 'original', True), ('restored', 'split', True)]
for root in (reference, current):
    inputs = json.loads((root/'inputs.json').read_text())
    assert inputs['visibility_input'] and inputs['visibility']
    if root == reference:
        assert inputs['version'] == 'herdr 0.2.57'
    samples = json.loads((root/'remote-visibility.json').read_text())
    assert len(samples) == len(expected)
    for sample, (phase, role, visible) in zip(samples, expected):
        assert (sample['phase'], sample['role']) == (phase, role)
        result = sample['response']['result']
        assert result['pane_visible'] == visible
        assert (result['cell_width_px'], result['cell_height_px']) == (8, 16)
        assert result['file_frame_damage'] and result['pixel_mouse']
    for phase, marker in [('split-original-focused', 'VIS_SPLIT_READY'),
                          ('split-new-focused', 'VIS_FOCUS_READY'),
                          ('zoom-new', 'VIS_ZOOM_READY'), ('restored', 'VIS_RESTORE_READY')]:
        screen = json.loads((root/('visibility-'+phase+'.json')).read_text())
        text = '\n'.join(''.join(cell.get('Content', '') for cell in row) for row in screen['Cells'])
        assert marker in text, (phase, 'real pane output did not reach the viewer')
    cleanup = json.loads((root/'cleanup.json').read_text())
    assert len(cleanup) == 2 and all(row['reply']['result']['type'] == 'ok' for row in cleanup)
    replies.append(samples)
assert replies[0] == replies[1], replies
report = {'passed': True, 'scope': 'actual saved-viewer split/focus/zoom and graphics API replies',
          'reference': str(reference), 'current': str(current), 'samples': replies[1]}
(current/'visibility-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
