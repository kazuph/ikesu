#!/bin/sh
set -eu
out=$1
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
mkdir -p "$out"
python3 - "$out" <<'PY'
import json
from pathlib import Path
import subprocess
import sys
out=Path(sys.argv[1])
proof=[]
for name,command in [('gofmt',['gofmt','-l','cmd','internal','tests','tools']),
                     ('vet',['go','vet','./...']),('test',['go','test','./...']),
                     ('race',['go','test','-race','./...'])]:
    result=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    (out/(name+'.log')).write_bytes(result.stdout)
    passed=result.returncode==0 and (name!='gofmt' or not result.stdout.strip())
    proof.append({'name':name,'command':command,'exit':result.returncode,'status':'PASS' if passed else 'FAIL'})
    print(proof[-1],flush=True)
(out/'gates.json').write_text(json.dumps(proof,indent=2)+'\n')
raise SystemExit(any(x['status']!='PASS' for x in proof))
PY
