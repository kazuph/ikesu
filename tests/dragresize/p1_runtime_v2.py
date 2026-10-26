#!/usr/bin/env python3
"""Diagnose P1 retained/unused heap. These instrumented runs are not acceptance."""
import argparse,hashlib,json,os,shutil,subprocess,time
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['before-source','out','revision']:p.add_argument('--'+name,required=True)
a=p.parse_args();source=Path(__file__).resolve().parents[2];before=Path(a.before_source).resolve();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
if not (before/'go.mod').is_file():raise FileNotFoundError(before/'go.mod')
cache=json.loads(subprocess.check_output(['go','env','-json','GOCACHE','GOMODCACHE'],text=True))
env={k:v for k,v in os.environ.items() if not k.startswith(('IKESU','HERDR','GTX','CLAUDE','CODEX'))}
for name in ['home','config','state','data','runtime']:(out/name).mkdir()
env.update(HOME=str(out/'home'),XDG_CONFIG_HOME=str(out/'config'),XDG_STATE_HOME=str(out/'state'),XDG_DATA_HOME=str(out/'data'),**cache)
baseline=out/'baseline-source'
shutil.copytree(before,baseline,ignore=shutil.ignore_patterns('.git','.artifacts','.worktree','__pycache__','*.test'))
# Add the same diagnostic sampler only; preserve the baseline's product code.
sampler=Path('cmd/ikesu/bench_stats_pprof.go')
shutil.copyfile(source/sampler,baseline/sampler)
with (out/'commands.jsonl').open('w') as log:
 def run(label,cwd,command):
  start=time.clock_gettime_ns(time.CLOCK_MONOTONIC);load=os.getloadavg()
  with (out/(label+'.log')).open('w') as output:result=subprocess.run(command,cwd=cwd,env=env,stdout=output,stderr=subprocess.STDOUT)
  row=dict(label=label,cwd=str(cwd),command=command,revision=a.revision,started_ns=start,finished_ns=time.clock_gettime_ns(time.CLOCK_MONOTONIC),load_before=load,load_after=os.getloadavg(),exit=result.returncode,diagnostic_only=True)
  log.write(json.dumps(row)+'\n');log.flush();print(json.dumps(row),flush=True)
  return result.returncode
 for name,cwd in [('before',baseline),('after',source)]:
  if run(name+'-build',cwd,['go','build','-tags=pprof','-o',str(out/name),'./cmd/ikesu']):raise RuntimeError('diagnostic build failed')
 (out/'metadata.json').write_text(json.dumps(dict(diagnostic_only=True,before_source=str(before),revision=a.revision,sampler_sha256=hashlib.sha256((source/sampler).read_bytes()).hexdigest(),binaries={name:hashlib.sha256((out/name).read_bytes()).hexdigest() for name in ['before','after']},gc_env={k:env.get(k) for k in ['GOGC','GOMEMLIMIT']}),indent=2)+'\n')
 env.update(IKESU_PERF_BIN=str(out/'after'),IKESU_PERF_BASELINE_BIN=str(out/'before'),IKESU_PERF_OUT=str(out/'relative-diagnostic.md'),IKESU_PERF_RUNTIME_SAMPLES=str(out/'runtime/{binary}-{pid}.jsonl'))
 code=run('p1-runtime-diagnostic',source,['go','test','-tags=perf','./tests/perf','-run=^TestRelative$','-count=1','-timeout=30m'])
 if not list((out/'runtime').glob('before-*.jsonl')) or not list((out/'runtime').glob('after-*.jsonl')):raise RuntimeError('missing per-process runtime samples')
raise SystemExit(code)
