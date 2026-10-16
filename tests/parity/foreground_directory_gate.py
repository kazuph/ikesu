"""Check actual spawn cwd after a persistent foreground process changes cwd."""
import json
from pathlib import Path
import sys

root = Path(sys.argv[1])
for role in ('herdr', 'gtx'):
    saved = json.loads((root/role/'runtime-json.json').read_text())
    spawned = json.loads(saved['home/cwd-evidence.json'])
    for name, directory, screen in (
            ('foreground-alpha-tab', 'alpha', 'newtab'),
            ('foreground-beta-split', 'beta', 'split-beta')):
        row = spawned[name]
        assert row['cwd'] == str(Path(row['home'])/directory), (role, name, row)
        assert 'DIRECTORY_'+name+'='+directory in (root/role/(screen+'.txt')).read_text()
    records = json.loads(saved['home/foreground-directory.json'])
    assert len(records) == 4, (role, records)
    for record in records:
        result = record['response']['result']
        if record['method'] == 'pane.get':
            assert Path(result['pane']['foreground_cwd']).name == record['phase'], (role, record)
        else:
            assert [w['label'] for w in result['workspaces']] == ['beta'], (role, record)
    for phase in ('alpha', 'beta'):
        sidebar = '\n'.join(line[:26] for line in
                            (root/role/('foreground-'+phase+'.txt')).read_text().splitlines())
        assert 'beta' in sidebar, (role, phase, sidebar)
print('PASS: foreground alpha tab, beta split, and shell-based API/sidebar names')
