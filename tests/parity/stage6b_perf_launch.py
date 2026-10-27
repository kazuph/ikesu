#!/usr/bin/env python3
# 比べる版は cmd/ikesu を持つ版（改名後）。
"""Select the approved measurement host and run the locked relative gate."""
from pathlib import Path
import json
import re
import subprocess
import sys

source=Path(__file__).resolve().parents[2]
archive=Path(sys.argv[1]).resolve()
load_text=subprocess.check_output(['ssh','mini','sysctl -n vm.loadavg'],text=True)
load=float(re.findall(r'[\d.]+',load_text)[0])
host='mini' if load < 4 else 'ubuntu-3090'
root='bench-terrace.noindex' if host=='mini' else 'bench-terrace'
remote_source=root+'/src/stage6b' if host=='mini' else root+'/work/stage6b-src'
destination=root+'/work/detail/stage6b-perf'
print(json.dumps({'mini_load':load,'host':host,'baseline':'70579fe','source':str(source)}),flush=True)
subprocess.run(['ssh',host,f'mkdir -p "$HOME/{destination}"'],check=True)
subprocess.run(['rsync','-az',str(archive),f'{host}:{destination}/baseline.tar'],check=True)
subprocess.run(['rsync','-az',str(source/'tests/parity/stage6b_perf.sh'),f'{host}:{destination}/run.sh'],check=True)
if host!='mini':
    subprocess.run(['rsync','-az','--delete','--exclude=/.worktree','--exclude=/.artifacts','--exclude=/.git','--exclude=/ikesu',str(source)+'/',f'{host}:{remote_source}/'],check=True)
setup=f'. "$HOME/{root}/env.sh"; ' if host!='mini' else ''
command=setup+f'"$HOME/{root}/work/perf-lock.py" sh "$HOME/{destination}/run.sh" "$HOME/{root}" "$HOME/{remote_source}"'
result=subprocess.run(['ssh',host,command])
local=archive.parent/'perf'
local.mkdir(exist_ok=True)
subprocess.run(['rsync','-az','--include=relative.md','--include=build.txt','--exclude=*',f'{host}:{destination}/',str(local)+'/'],check=True)
(local/'launch.json').write_text(json.dumps({'host':host,'mini_load':load,'exit':result.returncode,'command':command},indent=2)+'\n')
raise SystemExit(result.returncode)
