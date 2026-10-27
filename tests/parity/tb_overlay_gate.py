#!/usr/bin/env python3
"""Compare real outer image placements before, away from, and under a menu."""
import argparse
import base64
import json
from pathlib import Path
import re

p = argparse.ArgumentParser()
p.add_argument('out', type=Path)
p.add_argument('--dialog', action='store_true')
p.add_argument('--toast', action='store_true')
p.add_argument('--full', action='store_true', help='one source completely covered by the menu and then restored')
p.add_argument('--standalone-mode', action='store_true', help='full-mode hiding deletes legacy PTY image data and restores it by upload')
p.add_argument('--standalone-toast', action='store_true', help='no-session reload toast keeps placements and ignores targetless prefix+o')
a = p.parse_args()
results = {}
for name in ('herdr', 'ikesu'):
    root = a.out/name
    raw = (root/'terminal.raw').read_bytes()
    images, placements, stages, uploads = {}, {}, {}, {}
    cursor, start = (0, 0), 0
    for stage, end in re.findall(r'stats (\S+).*?outer_bytes_so_far=(\d+)', (root/'run.log').read_text()):
        count = 0
        for m in re.finditer(rb'\x1b\[(\d+);(\d+)H|\x1b_G(.*?)\x1b\\', raw[start:int(end)], re.S):
            if m[1] is not None:
                cursor = (int(m[2])-1, int(m[1])-1)
                continue
            header, _, payload = m[3].partition(b';')
            keys = dict(field.split(b'=', 1) for field in header.split(b',') if b'=' in field)
            action = keys.get(b'a', b't')
            image_id, placement_id = int(keys.get(b'i', b'0')), int(keys.get(b'p', b'0'))
            if action == b't':
                if keys.get(b'm', b'0') != b'0' or int(keys.get(b'f', b'32')) != 24:
                    raise SystemExit('unsupported fixture upload')
                count += 1
                images[image_id] = (int(keys[b's']), int(keys[b'v']), base64.b64decode(payload, validate=True).hex())
            elif action == b'p':
                placements[image_id, placement_id] = (cursor, keys)
            elif action == b'd':
                mode = keys.get(b'd', b'a')
                placements = {k: v for k, v in placements.items()
                              if not ((mode in (b'I', b'i') and k[0] == image_id and (not placement_id or k[1] == placement_id))
                                      or mode in (b'a', b'A'))}
        records = []
        for (image_id, _), (position, keys) in placements.items():
            width, height, pixels = images[image_id]
            crop = (int(keys.get(b'x', b'0')), int(keys.get(b'y', b'0')),
                    int(keys.get(b'w', str(width).encode())), int(keys.get(b'h', str(height).encode())))
            records.append({'position': position, 'cells': (int(keys[b'c']), int(keys[b'r'])), 'source': crop,
                            'offset': (int(keys.get(b'X', b'0')), int(keys.get(b'Y', b'0'))), 'pixels': pixels})
        stages[stage] = sorted(records, key=lambda item: json.dumps(item, sort_keys=True))
        uploads[stage] = count
        start = int(end)
    results[name] = {'stages': stages, 'uploads': uploads}
reference = results['herdr']['stages']
if a.full:
    changed = ('overlay-full', 'overlay-restored')
    reference_valid = (len(reference['overlay-base']) == 1 and not reference['overlay-full']
                       and reference['overlay-restored'] == reference['overlay-base'])
elif a.standalone_toast:
    changed = ('overlay-toast', 'overlay-restored')
    reference_valid = (len(reference['overlay-base']) == 2
                       and all(reference[stage] == reference['overlay-base'] for stage in changed))
    for name in ('herdr', 'ikesu'):
        for filename in ('overlay-toast.txt', 'overlay-after-targetless-key.txt'):
            reference_valid = reference_valid and 'reloaded config' in (a.out/name/filename).read_text()
elif a.dialog or a.toast:
    stage = 'overlay-toast' if a.toast else 'overlay-dialog'
    changed = (stage, 'overlay-restored')
    reference_valid = (len(reference['overlay-base']) == 2
                       and reference[stage] != reference['overlay-base']
                       and len(reference[stage]) > 0
                       and reference['overlay-restored'] == reference['overlay-base'])
else:
    changed = ('overlay-away', 'overlay-overlap')
    reference_valid = (len(reference['overlay-base']) == 2
                       and reference['overlay-away'] == reference['overlay-base']
                       and reference['overlay-overlap'] != reference['overlay-base']
                       and len(reference['overlay-overlap']) > 2)
passed = reference_valid and results['ikesu']['stages'] == reference
no_upload = all(r['uploads'][stage] == 0 for r in results.values() for stage in changed)
uploads_match = (all(r['uploads']['overlay-full'] == 0 and r['uploads']['overlay-restored'] == 1 for r in results.values())
                 if a.full and a.standalone_mode else no_upload)
report = {'reference_valid': reference_valid, 'placements_equal': passed, 'no_unchanged_uploads': no_upload,
          'expected_uploads_match': uploads_match, 'results': results}
(a.out/'overlay-gate.json').write_text(json.dumps(report, indent=2)+'\n')
print(json.dumps({key: report[key] for key in ('reference_valid', 'placements_equal', 'no_unchanged_uploads')}))
raise SystemExit(0 if passed and uploads_match else 1)
