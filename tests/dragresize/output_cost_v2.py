#!/usr/bin/env python3
"""Measure existing real-emulator throughput benchmarks, interleaved by build."""
import argparse,json,os,subprocess,time
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['before','after','out']:p.add_argument('--'+name,required=True)
p.add_argument('--bench',default='BenchmarkByteBoundedRecordedThroughput')
p.add_argument('--package',default='./internal/runtime')
p.add_argument('--test',default='TestHistoryReplayDoesNotExecuteTerminalRequests')
p.add_argument('--benchtime',help='Pass an explicit Go benchmark duration or iteration count')
a=p.parse_args();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
with (out/'measurements.jsonl').open('w') as log:
 for repeat in range(3):
  for phase in ['before','after']:
   source=Path(getattr(a,phase)).resolve();before=os.getloadavg()
   command=['go','test',a.package,'-run='+a.test,'-bench='+a.bench,'-benchmem','-count=1']
   if a.benchtime:command.append('-benchtime='+a.benchtime)
   result=subprocess.run(command,cwd=source,capture_output=True,text=True)
   record=dict(phase=phase,repeat=repeat+1,source=str(source),load_before=before,load_after=os.getloadavg(),clock_ns=time.clock_gettime_ns(time.CLOCK_MONOTONIC),exit=result.returncode,stdout=result.stdout,stderr=result.stderr)
   log.write(json.dumps(record)+'\n');log.flush();print(json.dumps(record),flush=True)
   if result.returncode:raise RuntimeError('output-cost benchmark failed; preserve failed sample')
