#!/usr/bin/env python3
"""Acceptance aggregation: no missing values are removed from the pass set."""
import argparse,json,statistics
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('directory');a=p.parse_args();root=Path(a.directory);conditions=json.loads((root/'conditions.json').read_text());rows=[]
def median(xs):return statistics.median(xs) if xs and all(v is not None for v in xs) else None
def ratio_ok(value,reference,factor):return value is not None and reference is not None and value<=factor*reference
for c in conditions:
 products={}
 for product in ['ikesu','herdr']:
  files=sorted(root.glob(c['name']+'-r*-'+product+'/analysis.json'));values=[json.loads(f.read_text()) for f in files];entry=dict(trials=len(values),files=[str(f) for f in files])
  for key in ['slow','application','update_interval_ms','tracking_lag_ms']:
   entry[key]=dict(median=median([v[key]['median'] for v in values]),p95=median([v[key]['p95'] for v in values]),maximum=max((v[key]['maximum'] for v in values if v[key]['maximum'] is not None),default=None))
  entry['complete']=len(values)>=3 and all(v['input_count']==c['count'] for v in values)
  entry['all_slow_observed']=entry['complete'] and all(not v['missing_slow_observations'] and not v['missing_application_observations'] for v in values)
  entry['final_dimensions_verified']=entry['complete'] and all(v['final_dimensions_verified'] for v in values)
  entry['G2']=entry['all_slow_observed'] and all(v[key][0]['median_ms'] is not None and all(b['median_ms'] is not None and b['median_ms']<=1.1*v[key][0]['median_ms'] for b in v[key]) for v in values for key in ['blocks','app_blocks'])
  products[product]=entry
 candidate,reference=products['ikesu'],products['herdr'];base=candidate['complete'] and reference['complete'];gates={}
 if c['rate']==4:
  gates['G1']=base and candidate['all_slow_observed'] and reference['all_slow_observed'] and all(ratio_ok(candidate[k][q],reference[k][q],1.1) for k in ['slow','application'] for q in ['median','p95'])
  gates['G2']=candidate['G2']
 else:
  gates['G3']=base and ratio_ok(candidate['update_interval_ms']['maximum'],reference['update_interval_ms']['maximum'],1.2) and ratio_ok(candidate['update_interval_ms']['p95'],reference['update_interval_ms']['p95'],1.1) and ratio_ok(candidate['tracking_lag_ms']['p95'],reference['tracking_lag_ms']['p95'],1.1)
 gates['release_dimensions']=candidate['final_dimensions_verified']
 rows.append(dict(condition=c,products=products,gates=gates,pass_all=all(gates.values())))
(root/'comparison.json').write_text(json.dumps(rows,indent=2)+'\n')
for r in rows:print(r['condition']['name'],r['gates'])
