"""Hand off only the server owned by the current isolated parity scenario."""
import json
import os
from pathlib import Path
import subprocess

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('GTX_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: runner-owned socket required')
binary = Path(os.environ['PARITY_OBSERVE_BINARY']).resolve()
result = subprocess.run([str(binary), '--session', 'parity', 'server', 'live-handoff'],
                        capture_output=True, text=True, timeout=30)
(Path.home()/'directory-handoff.json').write_text(json.dumps(dict(
    command=result.args, exit=result.returncode, stdout=result.stdout, stderr=result.stderr), indent=2))
assert result.returncode == 0, (result.returncode, result.stdout, result.stderr)
print('DIRECTORY_HANDOFF_READY', flush=True)
