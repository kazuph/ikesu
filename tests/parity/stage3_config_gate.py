#!/usr/bin/env python3
"""Check saved real configurations and clipboard denial, without using pasteboard."""
import json
from pathlib import Path
import sys
import tomllib

root = Path(sys.argv[1])
cases = sorted(p for p in root.iterdir() if (p/'reference.json').is_file())
assert cases, 'no observations'
summary = []
for case in cases:
    reference = json.loads((case/'reference.json').read_text())
    assert reference['herdr_version'] == 'herdr 0.2.57', reference
    assert reference['sound_disabled'] is True, case
    for app in ('herdr', 'gtx'):
        saved = json.loads((case/app/'config-files.json').read_text())
        assert len(saved) == 1, (case, app, saved.keys())
        config = tomllib.loads(next(iter(saved.values())))
        assert config['keys']['prefix'] == 'ctrl+t', (case, app)
        popup = [c for c in config['keys']['command'] if c['key'] == 'prefix+t']
        assert len(popup) == 1 and popup[0]['type'] == 'popup', (case, app)
        assert popup[0]['width'] == popup[0]['height'] == '80%', (case, app)
        assert config['ui']['show_tab_bar'] is True, (case, app)
        assert config['theme']['name'] == 'tokyo-night', (case, app)
        assert config['theme']['custom'] == {
            'panel_bg':'transparent', 'surface_dim':'#16161e', 'surface0':'#24283c',
            'overlay0':'#737aa2', 'overlay1':'#a9b1d6'}, (case, app)
        sound = config['ui']['sound']
        assert sound['enabled'] is False and 'on' not in sound.get('agents', {}).values(), (case, app)
        launch = json.loads((case/app/'sandbox-launch.json').read_text())
        assert launch['launcher'][0] == '/usr/bin/sandbox-exec', (case, app)
        assert '(deny mach-lookup (global-name "com.apple.pasteboard.1"))' in launch['launcher'][2], (case, app)
    probe = json.loads((case/'clipboard-guard/sandbox-probe.json').read_text())
    assert probe['exit_code'] == 1 and probe['change_kind'] == 'not_read_sandboxed', case
    proof = json.loads((case/'clipboard-guard/proof.json').read_text())
    assert len(proof) == 2, case
    for row in proof + [probe]:
        assert row['before_sha256'] is None and row['after_sha256'] is None, case
    summary.append({'case': case.name, 'settings': 'PASS', 'sound': 'PASS', 'clipboard_sandbox': 'PASS'})

wheel = root/'stage3-wheel'
if wheel.is_dir():
    boxes = {}
    for app in ('herdr', 'gtx'):
        cells = json.loads((wheel/app/'popup.json').read_text())['Cells']
        starts = [(x,y) for y,row in enumerate(cells) for x,c in enumerate(row) if c.get('Content') == '┌']
        assert len(starts) == 2, (app, starts)
        rectangles = []
        for x,y in starts:
            right = next(i for i in range(x+1,len(cells[y])) if cells[y][i].get('Content') == '┐')
            bottom = next(i for i in range(y+1,len(cells)) if cells[i][x].get('Content') == '└')
            assert cells[bottom][right].get('Content') == '┘'
            rectangles.append((x,y,right-x+1,bottom-y+1))
        outer, popup = rectangles
        assert popup[2] == outer[2]*80//100 and popup[3] == outer[3]*80//100, (app, rectangles)
        boxes[app] = popup
    assert boxes['herdr'] == boxes['gtx'], boxes
    summary.append({'popup_dimensions': boxes, 'status':'PASS'})
(root/'config-gate.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2)+'\n')
print(f'PASS: {len(cases)} configurations, clipboard denial, sound, and observed popup dimensions')
