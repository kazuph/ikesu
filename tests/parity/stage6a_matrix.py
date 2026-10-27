#!/usr/bin/env python3
"""Collect real stage6a frames; collection success is not parity success."""
import argparse
from pathlib import Path
import subprocess
p=argparse.ArgumentParser();p.add_argument('--out',required=True);p.add_argument('--ikesu',dest='ikesu',type=Path);p.add_argument('--only',nargs='*');p.add_argument('--cell-clock',action='store_true');p.add_argument('--toast-delivery',choices=['off','terminal']);a=p.parse_args()
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
        if a.toast_delivery:
            config.write_text(config.read_text()+f'\n[ui.toast]\ndelivery = "{a.toast_delivery}"\n')
        else:
            extra=['--in-app-toast']
    result=subprocess.run(['python3',str(here/'observe.py'),'--herdr',str(work/'herdr-0.2.57'),'--ikesu',str(a.ikesu or work/'ikesu-stage6a'),'--ptyrun',str(work/'ptyrun'),'--config-file',str(config),'--steps',str(path),'--out',str(out/path.stem),'--clipboard-sandbox','--silent','--server']+extra+(['--cell-clock'] if a.cell_clock else []))
    if result.returncode: failed.append(path.stem)
print('FAILED COLLECTION:',failed,flush=True)
raise SystemExit(bool(failed))
