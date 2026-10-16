#!/usr/bin/env python3
"""Run generated stage-two procedures against isolated real servers."""
import argparse
from pathlib import Path
import subprocess
p=argparse.ArgumentParser();p.add_argument('--out',required=True);p.add_argument('--only',nargs='*');a=p.parse_args()
root=Path.home()/'bench-terrace.noindex';work=root/'work/detail';here=Path(__file__).resolve().parent
cases=Path(a.out)/'procedures'
subprocess.run(['python3',str(here/'stage2_cases.py'),str(cases),str(work/'user-config.toml')],check=True)
for path in sorted(cases.glob('*.steps')):
 if a.only and path.stem not in a.only: continue
 print(path.stem,flush=True)
 subprocess.run(['python3',str(here/'observe.py'),'--herdr',str(work/'herdr-0.2.57'),'--gtx',str(work/'gtx-stage2'),'--ptyrun',str(work/'ptyrun'),'--config-file',str(path.with_suffix('.toml')),'--steps',str(path),'--out',str(Path(a.out)/path.stem),'--clipboard-sandbox','--server'],check=True)
