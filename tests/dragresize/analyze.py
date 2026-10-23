#!/usr/bin/env python3
"""Summarize saved real terminal observations; missing samples stay missing."""
import json,re,statistics,sys,math,unicodedata
from pathlib import Path
base=Path(sys.argv[1]); rows=[]
def cpu_seconds(value):
 parts=value.split(':'); return sum(float(v)*60**i for i,v in enumerate(reversed(parts)))
def coverage(raw):
 text=raw.decode('utf8',errors='replace'); x=y=0; cells=set(); emitted=0; clear=0; i=0
 while i<len(text):
  if text[i]=='\x1b':
   if i+1<len(text) and text[i+1]=='[':
    m=re.match(r'\x1b\[([0-?]*)([ -/]*)([@-~])',text[i:])
    if not m: break
    params,_,final=m.groups(); nums=[int(n) if n else 1 for n in params.split(';')] if re.fullmatch(r'[0-9;]*',params) else []
    n=nums[0] if nums else 1
    if final in ('H','f') and nums: y=n-1; x=(nums[1] if len(nums)>1 else 1)-1
    elif final=='G' and nums:x=n-1
    elif final=='d' and nums:y=n-1
    elif final=='A' and nums:y-=n
    elif final=='B' and nums:y+=n
    elif final=='C' and nums:x+=n
    elif final=='D' and nums:x-=n
    elif final=='J' and params in ('2','3'): cells.update((a,b) for a in range(160) for b in range(45)); clear+=1
    elif final=='K': cells.update((a,y) for a in range(max(0,x),160))
    i+=len(m[0]);continue
   if i+1<len(text) and text[i+1] in ']P_':
    m=re.match(r'\x1b[][P_].*?(?:\x07|\x1b\\)',text[i:],re.S)
    if m:i+=len(m[0]);continue
   i+=2;continue
  c=text[i];i+=1
  if c=='\r':x=0
  elif c=='\n':y+=1
  elif ord(c)>=32 and not unicodedata.combining(c):
   width=2 if unicodedata.east_asian_width(c) in 'WF' else 1
   for d in range(width):
    if 0<=x+d<160 and 0<=y<45: cells.add((x+d,y))
   x+=width;emitted+=width
 return len(cells),emitted,clear
for path in sorted(base.iterdir()):
 if not path.is_dir() or not (path/'run.log').exists() or path.name.endswith('pilot') or path.name.endswith('profile'):continue
 log=(path/'run.log').read_text(); raw=(path/'outer.bin').read_bytes(); samples=[]
 for m in re.finditer(r'drag sample=(\d+) sent_us=(\d+) seen_ms=(\d+) bytes=(\d+) begin=(\d+) end=(\d+)',log):
  n,start,seen,size,begin,end=map(int,m.groups()); c,e,clear=coverage(raw[begin:end])
  samples.append(dict(n=n,sent_us=start,seen_ms=seen,latency_ms=seen-start/1000 if seen else None,bytes=size,coverage=c,emitted=e,clear=clear))
 if not samples:continue
 lat=sorted(s['latency_ms'] for s in samples if s['latency_ms'] is not None)
 cpus={}
 for stage,pid,value in re.findall(r'cpu (before|after) (\d+)\s+([\d:.]+)\s+[\d.]+',log):cpus.setdefault(pid,{})[stage]=cpu_seconds(value)
 duration=(samples[-1]['sent_us']-samples[0]['sent_us'])/1e6+.25+.1
 cpu=[round(100*(v['after']-v['before'])/duration,2) for v in cpus.values() if 'after'in v and 'before'in v]
 resize=[]; repaint=[]
 for f in path.glob('*.resize'):
  stamps=[]
  for l in f.read_text().splitlines():
   try:
    t=float(l.split()[0]); stamps.append(t/1e9 if t>1e15 else t)
   except ValueError: pass
  counts=[sum(s['sent_us']/1e6<=t<s['sent_us']/1e6+.25 for t in stamps) for s in samples];resize.append(dict(pane=f.stem,total=sum(counts),per_motion=counts))
 result=dict(case=path.name,n=len(samples),observed=len(lat),median_ms=statistics.median(lat) if lat else None,p95_ms=lat[math.ceil(.95*len(lat))-1] if lat else None,bytes_median=statistics.median(s['bytes'] for s in samples),coverage_median=statistics.median(s['coverage'] for s in samples),emitted_median=statistics.median(s['emitted'] for s in samples),clear_total=sum(s['clear'] for s in samples),cpu_client_server=cpu,resize=resize,load=(path/'load.txt').read_text() if (path/'load.txt').exists() else None,samples=samples)
 rows.append(result)
(base/'summary.json').write_text(json.dumps(rows,indent=2)+'\n')
for r in rows:
 print(r['case'],f"{r['observed']}/{r['n']}",r['median_ms'],r['p95_ms'],r['bytes_median'],r['coverage_median'],r['cpu_client_server'],[p['total'] for p in r['resize']])
