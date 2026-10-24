#!/usr/bin/env python3
"""Measure existing real-emulator throughput benchmarks, interleaved by build."""
import argparse,json,os,subprocess,time
from pathlib import Path

p=argparse.ArgumentParser()
for name in ['before','after','out']:p.add_argument('--'+name,required=True)
a=p.parse_args();out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=False)
with (out/'measurements.jsonl').open('w') as log:
 for repeat in range(3):
  for phase in ['before','after']:
   source=Path(getattr(a,phase)).resolve();before=os.getloadavg()
   result=subprocess.run(['go','test','./internal/runtime','-run=TestHistoryReplayDoesNotExecuteTerminalRequests','-bench=BenchmarkByteBoundedRecordedThroughput','-benchmem','-count=1'],cwd=source,capture_output=True,text=True)
   record=dict(phase=phase,repeat=repeat+1,source=str(source),load_before=before,load_after=os.getloadavg(),clock_ns=time.clock_gettime_ns(time.CLOCK_MONOTONIC),exit=result.returncode,stdout=result.stdout,stderr=result.stderr)
   log.write(json.dumps(record)+'\n');log.flush();print(json.dumps(record),flush=True)
   if result.returncode:raise RuntimeError('output-cost benchmark failed; preserve failed sample')
