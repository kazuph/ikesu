"""Assert actual inherited directories; terminal screenshots alone are insufficient."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
expected = {'keys-alpha': 'alpha', 'plus-beta': 'beta',
            'vertical-osc-alpha': 'alpha', 'horizontal-beta': 'beta',
            'login-alpha': 'alpha', 'menu-beta': 'beta'}
if len(sys.argv) > 2:
    assert sys.argv[2] == 'context', 'unsupported directory proof case'
    expected = {'worktree-tab': 'feature', 'switched-tab': 'feature', 'switched-split': 'feature'}
for role in ['herdr', 'ikesu']:
    saved = json.loads((root/role/'runtime-json.json').read_text())
    rows = json.loads(saved['home/cwd-evidence.json'])
    assert set(rows) == set(expected), (role, set(rows), set(expected))
    for name, directory in expected.items():
        row = rows[name]
        want = str(Path(row['home'])/directory)
        assert row['cwd'] == want, (role, name, row['cwd'], want)
        screen = (root/role/(name+'.txt')).read_text()
        assert 'DIRECTORY_'+name+'='+directory in screen, (role, name, 'output absent')
print(f'PASS: both products inherited the exact selected directory in all {len(expected)} UI actions')
