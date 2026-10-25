#!/usr/bin/env python3
"""Run both module checks and SPEC P1 in an isolated bench environment."""
import argparse,hashlib,json,os,subprocess,time
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['before-source','out','revision']:p.add_argument('--'+name,required=True)
p.add_argument('--migration-baseline',required=True,help='Real binary built from the legacy migration commit')
a=p.parse_args();source=Path(__file__).resolve().parents[2];before=Path(a.before_source).resolve();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
cache=json.loads(subprocess.check_output(['go','env','-json','GOCACHE','GOMODCACHE'],text=True))
env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','GTX','CLAUDE','CODEX'))}
for name in ['home','config','state','data']:(out/name).mkdir()
env.update(HOME=str(out/'home'),XDG_CONFIG_HOME=str(out/'config'),XDG_STATE_HOME=str(out/'state'),XDG_DATA_HOME=str(out/'data'),**cache)
env.update(IKESU_PERF_BIN=str(out/'candidate'),IKESU_PERF_BASELINE_BIN=str(out/'baseline-main-b83c38e'),IKESU_PERF_OUT=str(out/'relative.md'))
migration=Path(a.migration_baseline).resolve()
if not migration.is_file():raise FileNotFoundError(migration)
# The legacy executable selects its state namespace from argv[0]. Match the
# existing migration fixture's executable name without modifying its bytes.
legacy=out/'gtx-2e01183';legacy.symlink_to(migration)
env['IKESU_MIGRATION_BASE_BIN']=str(legacy)
(out/'migration-baseline.json').write_text(json.dumps(dict(path=str(migration),invocation_path=str(legacy),sha256=hashlib.sha256(migration.read_bytes()).hexdigest()),indent=2)+'\n')
failed=False
with (out/'commands.jsonl').open('w') as log:
 def run(label,cwd,command):
  start=time.clock_gettime_ns(time.CLOCK_MONOTONIC);load=os.getloadavg()
  with (out/(label+'.log')).open('w') as output:result=subprocess.run(command,cwd=cwd,env=env,stdout=output,stderr=subprocess.STDOUT)
  row=dict(label=label,cwd=str(cwd),command=command,revision=a.revision,started_ns=start,finished_ns=time.clock_gettime_ns(time.CLOCK_MONOTONIC),load_before=load,load_after=os.getloadavg(),exit=result.returncode)
  log.write(json.dumps(row)+'\n');log.flush();print(json.dumps(row),flush=True)
  return result.returncode==0
 builds=run('baseline-build',before,['go','build','-o',str(out/'baseline-main-b83c38e'),'./cmd/ikesu'])
 builds=run('candidate-build',source,['go','build','-o',str(out/'candidate'),'./cmd/ikesu']) and builds
 for label,cwd in [('root',source),('vt',source/'third_party/vt')]:
  failed=not run(label+'-vet',cwd,['go','vet','./...']) or failed
 for race in [False,True]:
  for label,cwd in [('root',source),('vt',source/'third_party/vt')]:
   command=['go','test',*(['-race'] if race else []),'-count=1','-timeout=30m','./...']
   failed=not run(label+('-race' if race else '-tests'),cwd,command) or failed
 if builds:
  hashes={name:hashlib.sha256((out/name).read_bytes()).hexdigest() for name in ['baseline-main-b83c38e','candidate']}
  (out/'binaries.json').write_text(json.dumps(hashes,indent=2)+'\n')
  failed=not run('p1-relative',source,['go','test','-tags=perf','./tests/perf','-run=^TestRelative$','-count=1','-timeout=30m']) or failed
 else:failed=True
raise SystemExit(1 if failed else 0)
