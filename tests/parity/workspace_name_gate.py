"""Check API labels and sidebar names against the observed reference rule."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
case = sys.argv[2]
expected = {
    'focus': [('cd', 'alpha'), ('right-beta', 'alpha'),
              ('left-alpha', 'alpha'), ('left-beta', 'beta')],
    'repository': [('sub', 'repo'), ('worktree', 'feature')],
    'custom': [('automatic', 'alpha'), ('custom', 'chosen')],
    'handoff': [('name', 'alpha')],
}[case]
for role in ['herdr', 'ikesu']:
    saved = json.loads((root/role/'runtime-json.json').read_text())
    actions = json.loads(saved['home/parity-api-actions.json'])
    assert len(actions) == len(expected), (role, len(actions), len(expected))
    for action, (screen_name, name) in zip(actions, expected):
        spaces = action['response']['result']['workspaces']
        if case == 'custom':
            labels = [w['label'] for w in spaces]
            want = ['alpha', 'chosen'] if screen_name == 'custom' else ['alpha']
            assert labels == want, (role, screen_name, labels, want)
        else:
            active = [w for w in spaces if w['focused']]
            assert len(active) == 1 and active[0]['label'] == name, (role, screen_name, active, name)
        screen = (root/role/(screen_name+'.txt')).read_text()
        sidebar = '\n'.join(line[:26] for line in screen.splitlines())
        assert name in sidebar, (role, screen_name, sidebar)
print('PASS: API and sidebar match the reference automatic/custom naming rule: '+case)
