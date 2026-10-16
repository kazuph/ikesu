"""Record the real directory inherited by a shell command after a UI action."""
import json
from pathlib import Path
import sys

path = Path.home()/'cwd-evidence.json'
rows = json.loads(path.read_text()) if path.exists() else {}
name = sys.argv[1]
if name in rows:
    raise SystemExit('duplicate directory observation: '+name)
rows[name] = dict(cwd=str(Path.cwd().resolve()), home=str(Path.home().resolve()))
path.write_text(json.dumps(rows, indent=2))
print('DIRECTORY_'+name+'='+Path.cwd().name, flush=True)
