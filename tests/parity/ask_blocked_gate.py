#!/usr/bin/env python3
"""Check captured real-agent status cells, API transitions and notification bytes."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
# Default Catppuccin palette, internal/render/palette.go.
RED = {'R':243,'G':139,'B':168,'A':255}
YELLOW = {'R':249,'G':226,'B':175,'A':255}
results = {}
for name in ['herdr','gtx']:
    base = root/name
    summary = json.loads((base/'summary.json').read_text())
    assert summary['status']=='PASS', summary
    assert [summary[k] for k in ['before','question','restored']]==['working','blocked','working']
    assert summary['sound_disabled'] is True
    frames = {phase:json.loads((base/(phase+'.cells.json')).read_text())['Cells']
              for phase in ['before','question','restored']}
    borders = []
    counts = {}
    for phase, cells in frames.items():
        y,x = next((y,x) for y,row in enumerate(cells) for x,c in enumerate(row) if c['Content']=='┌')
        borders.append(cells[y][x]['Style']['Fg'])
        expected = RED if phase=='question' else YELLOW
        tab = [c for c in cells[0][x:] if c['Style'].get('Fg')==expected]
        sidebar = [c for row in cells for c in row[:x] if c['Style'].get('Fg')==expected]
        assert tab and sidebar, (name,phase,'status color missing')
        if phase=='question':
            assert any(c['Content']=='●' for c in tab)
            assert any(c['Content']=='●' for c in sidebar)
        counts[phase]={'tab_status_color_cells':len(tab),'sidebar_status_color_cells':len(sidebar)}
    assert borders[0]==borders[1]==borders[2] and borders[1]!=RED, (name,'border changed with status',borders)
    question=(base/'question.txt').read_text()
    assert 'Probe first' in question and 'Esc to cancel' in question, (name,'real question absent')
    raw=(base/'terminal.raw').read_bytes()
    notifications=raw.count(b'\x1b]9;')
    assert notifications==1, (name,'expected exactly one terminal notification',notifications)
    if name=='gtx':
        messages=[json.loads(line) for line in (base/'client-protocol.jsonl').read_text().splitlines()]
        requests=[m['notification'] for m in messages if m.get('type')=='notify']
        assert len(requests)==1, requests
        request=requests[0]
        assert request['kind']=='needs_attention' and request['agent']=='claude' and request['target_pane_id']==summary['pane'], request
    results[name]={'status':'PASS','api':'working -> blocked -> working','cells':counts,
                   'pane_border_rgb':borders[0],'terminal_notification_requests':notifications,
                   'version':summary['binary_version'],'sound':'disabled; sound demand verified by Go notification contract'}
assert results['herdr']['pane_border_rgb']==results['gtx']['pane_border_rgb']
print(json.dumps(results,ensure_ascii=False,indent=2))
