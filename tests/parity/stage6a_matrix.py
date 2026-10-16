#!/usr/bin/env python3
"""Collect real stage6a frames; collection success is not parity success."""
import argparse
from pathlib import Path
import subprocess
p=argparse.ArgumentParser();p.add_argument('--out',required=True);p.add_argument('--only',nargs='*');a=p.parse_args()
root=Path.home()/'bench-terrace.noindex';work=root/'work/detail';here=Path(__file__).resolve().parent
out=Path(a.out);cases=out/'procedures'
subprocess.run(['python3',str(here/'stage6a_cases.py'),str(cases)],check=True)
failed=[]
for path in sorted(cases.glob('*.steps')):
    if a.only and path.stem not in a.only: continue
    print(path.stem,flush=True)
    config=work/'user-config.toml'
    extra=[]
    if path.stem.startswith('toast-'):
        config=cases/'toast.toml'
        config.write_text((work/'user-config.toml').read_text().replace('[ui.toast]\ndelivery = "system"',''))
        extra=['--in-app-toast']
    result=subprocess.run(['python3',str(here/'observe.py'),'--herdr',str(work/'herdr-0.2.57'),'--gtx',str(work/'gtx-stage6a'),'--ptyrun',str(work/'ptyrun'),'--config-file',str(config),'--steps',str(path),'--out',str(out/path.stem),'--clipboard-sandbox','--server']+extra)
    if result.returncode: failed.append(path.stem)
print('FAILED COLLECTION:',failed,flush=True)
raise SystemExit(bool(failed))
