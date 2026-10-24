"""Verify the observed herdr 0.2.66 ownership transitions for both products."""
import json, pathlib, sys
root = pathlib.Path(sys.argv[1]).resolve()
failures = []
checks = []

def check(label, condition, actual):
    checks.append(dict(check=label, pass_=bool(condition), actual=actual))
    if not condition:
        failures.append(label)

def load(name):
    return {r['phase']: r['sizes'] for r in json.loads((root / name / 'observations.json').read_text())}
ref = load('reference-final')
after = load(sys.argv[2] if len(sys.argv) > 2 else 'after-final')
extra_ref = load('reference-extra')
extra_after = load(sys.argv[3] if len(sys.argv) > 3 else 'after-extra-final')
triggers = {'hover': True, 'press': False, 'release': False, 'wheel': True, 'key': True, 'paste': True, 'focus-in': True, 'focus-out': False}
for product, data in [('herdr', ref), ('ikesu', after)]:
    calib = {key: tuple(map(int, data[phase]['p1'].split())) for key, phase in [('A', 'A-only'), ('B', 'B-connect'), ('C', 'C-connect')]}

    def expected(owner, split=False, resized=False):
        h, w = calib[owner]
        if resized:
            h += 5
            w += 14
        if split:
            w = (w - 3) // 2
        return f'{h} {w}'
    for prefix, split in [('two', False), ('three', False), ('split-three', True), ('hidden', True)]:
        for event, claims in triggers.items():
            phase = prefix + '-A-' + event
            want = expected('A' if claims else 'B', split)
            for pid in ['p1', 'p2'] if split else ['p1']:
                check(product + ':' + phase + ':' + pid, data[phase][pid] == want, dict(got=data[phase][pid], want=want))
            if prefix == 'hidden':
                for pid in ['p3', 'p4']:
                    check(product + ':hidden-invariant:' + event + ':' + pid, data[phase][pid] == data['hidden-created'][pid], data[phase][pid])
    for phase, owner, resized in [('A-resize', 'A', True), ('B-key-after-resize', 'B', False), ('B-disconnect-two-remain', 'A', True), ('A-hover-after-disconnect', 'A', True), ('C-disconnect-one-remains', 'A', True)]:
        for pid in ['p1', 'p2']:
            want = expected(owner, True, resized)
            check(product + ':' + phase + ':' + pid, data[phase][pid] == want, dict(got=data[phase][pid], want=want))
        for pid in ['p3', 'p4']:
            check(product + ':hidden-tail:' + phase + ':' + pid, data[phase][pid] == data['hidden-created'][pid], data[phase][pid])
for product, data in [('herdr', extra_ref), ('ikesu', extra_after)]:
    a = data['A-only']['p1']
    b = data['B-connect']['p1']
    expected = {'B-last-key': {'p1': b}, 'B-detached-two-remain': {'p1': a}, 'A-hover-after-detach': {'p1': a}, 'C-detached-one-remains': {'p1': a}, 'B-select-second-tab': {'p1': a, 'p2': b}, 'A-key-other-tab': {'p1': a, 'p2': b}}
    ah, aw = map(int, a.split())
    resized = f'{ah + 5} {aw + 14}'
    expected.update({'A-resize-other-tab': {'p1': resized, 'p2': b}, 'B-key-own-tab': {'p1': resized, 'p2': b}, 'A-select-second-tab': {'p1': resized, 'p2': resized}})
    for phase, want in expected.items():
        check(product + ':extra:' + phase, data[phase] == want, dict(got=data[phase], want=want))
result = dict(total=len(checks), passed=len(checks) - len(failures), failures=failures, checks=checks)
(root / 'verification-final.json').write_text(json.dumps(result, indent=2))
print(json.dumps({k: v for k, v in result.items() if k != 'checks'}))
raise SystemExit(bool(failures))
