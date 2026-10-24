#!/usr/bin/env python3
"""Run the parent's existing-cwd method, with its explicitly authorized retry."""
import argparse,json,subprocess,sys
from pathlib import Path

p=argparse.ArgumentParser();p.add_argument('--out',required=True);a,remaining=p.parse_known_args()
out=Path(a.out).resolve();helper=Path(__file__).with_name('claude_wait.py')
command=[sys.executable,str(helper),*remaining]
result=subprocess.run([*command,'--out',str(out)])
if result.returncode==0:sys.exit(0)
if not (out/'fallback-needed').exists():sys.exit(result.returncode)
cleanup=json.loads((out/'cleanup.json').read_text())
if not all(cleanup.get(key) for key in ['claude_normal_exit','original_session_unchanged','copied_sessions_removed','temporary_workdirs_removed']):
 raise RuntimeError('ordinary exit and cleanup must finish before authorized retry')
retry=Path(str(out)+'-temporary')
sys.exit(subprocess.run([*command,'--temporary-cwd','--out',str(retry)]).returncode)
