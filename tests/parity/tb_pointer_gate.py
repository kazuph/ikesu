#!/usr/bin/env python3
"""Compare real saved-tb PTY input and browser DOM states, without rescaling."""
import json
from pathlib import Path
import re
import sys

reference, current = map(Path, sys.argv[1:])
inputs = [json.loads((root/'inputs.json').read_text()) for root in (reference, current)]
assert inputs[0]['version'] == 'herdr 0.2.57'
assert all(item['pointer_probe'] and item['tap_tb_input'] for item in inputs)
assert inputs[0]['pixel_mouse'] == inputs[1]['pixel_mouse']
records = [json.loads((root/'pointer-dom.json').read_text()) for root in (reference, current)]
expected = ['hover', 'leave', 'right', 'left']
if inputs[0]['pixel_mouse']:
    expected += ['fine-a', 'fine-b']
for root, events in zip((reference, current), records):
    assert [event['stage'] for event in events] == expected
    assert events[0]['state']['hover'] and not events[1]['state']['hover']
    assert events[2]['state']['scrollX'] == events[2]['state']['wheelX'] > 0
    assert events[3]['state']['scrollX'] == 0 and events[3]['state']['wheelX'] < 0
    if inputs[0]['pixel_mouse']:
        assert json.loads((root/'pointer-pixel-mode.json').read_text())['actual_outer_pixel_mode']
        assert events[-1]['tb_pty_delta'] == [4, 4]
    cleanup = json.loads((root/'cleanup.json').read_text())
    assert len(cleanup) == 2 and all(row['reply']['result']['type'] == 'ok' for row in cleanup)
mouse = [re.findall(rb'\x1b\[<\d+;\d+;\d+[Mm]', (root/'remote-tb-input.bin').read_bytes())
         for root in (reference, current)]
assert len(mouse[0]) == len(expected) and mouse[0] == mouse[1]
assert records[0] == records[1], records
report = {'passed': True, 'actual_tb_mouse_bytes_equal': True, 'dom_event_states_equal': True,
          'scope': 'PTY precision and real DOM events; native appearance unverified',
          'reference': str(reference), 'current': str(current), 'events': records[1]}
(current/'pointer-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps(report))
