#!/usr/bin/env python3
"""Compare real footer cells, using the existing isolated PTY observer."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys
import tomllib

p=argparse.ArgumentParser()
p.add_argument('--gtx',required=True)
p.add_argument('--herdr',required=True)
p.add_argument('--ptyrun',required=True)
p.add_argument('--config',required=True)
p.add_argument('--out',required=True)
a=p.parse_args()
out=Path(a.out).resolve();out.mkdir(parents=True,exist_ok=True)
config=Path(a.config).read_text()
parsed=tomllib.loads(config)
if 'sound' not in parsed.get('ui',{}):config+='\n[ui.sound]\nenabled=false\n'
else:
    assert parsed['ui']['sound']['enabled'] is False, 'comparison config must disable sound'
(out/'config.toml').write_text(config)
widths=[160]+list(range(110,64,-1))+[50]
steps=[]
for width in widths:
    steps += [f'resize {width} 45','sleep 100ms',f'screen @OUT@/width-{width}.txt',f'cells @OUT@/width-{width}.json']
steps+=['resize 160 3','sleep 100ms','screen @OUT@/height-3.txt','cells @OUT@/height-3.json','kill']
procedure=out/'matrix.steps';procedure.write_text('\n'.join(steps)+'\n')
records=[]
for mode in ['server','standalone']:
    target=out/mode
    command=[sys.executable,str(Path(__file__).with_name('observe.py')),'--gtx',a.gtx,'--herdr',a.herdr,'--ptyrun',a.ptyrun,'--steps',str(procedure),'--out',str(target),'--config-file',str(out/'config.toml'),'--silent','--clipboard-sandbox']
    if mode=='server':command+=['--server','--server-reuse-initial']
    subprocess.run(command,check=True)
    for width in widths:
        entries={}
        for product in ['herdr','gtx']:
            doc=json.loads((target/product/f'width-{width}.json').read_text())
            row=doc['Cells'][-1]
            text=''.join(cell['Content'] or ' ' for cell in row)
            copy=text.find(' COPY ');cycle=text.find(' CYCLE LAYOUT ')
            version=re.search(r'\b'+product+r' v[^ ]+',text)
            if copy<0 or cycle<0:
                assert version is None,(mode,width,product,'version without buttons',text)
                continue
            label=product+' v'+('0.1.0' if product=='gtx' else '0.2.61')
            left,right=copy+len(' COPY '),cycle
            fits=right-left>=len(label)+2
            assert bool(version)==fits,(mode,width,product,'visibility',text)
            if version:
                l,r=version.start()-left,right-version.end()
                assert min(l,r)>=1 and abs(l-r)<=1,(mode,width,product,l,r)
                assert version.group()==label,(product,version.group())
            entries[product]={'copy':copy,'cycle':cycle,'rotate':text.find(' ROTATE PANES '),'equalize':text.find(' EQUALIZE '),'gap':right-left,'label_width':len(label),'version':version.start() if version else None,'style':row[version.start()]['Style'] if version else None}
        if len(entries)==2:
            for key in ['copy','cycle','rotate','equalize','gap']:
                assert entries['herdr'][key]==entries['gtx'][key],(mode,width,key,entries)
            if entries['herdr']['style'] is not None and entries['gtx']['style'] is not None:
                assert entries['herdr']['style']==entries['gtx']['style'],(mode,width,'style',entries)
        records.append({'mode':mode,'width':width,'products':entries})
    for product in ['herdr','gtx']:
        doc=json.loads((target/product/'height-3.json').read_text())
        text='\n'.join(''.join(cell['Content'] or ' ' for cell in row) for row in doc['Cells'])
        if product=='gtx':assert 'gtx v0.1.0' not in text,'version appeared without a footer'
for product in ['herdr','gtx']:
    for mode in ['server','standalone']:
        candidates=[r['products'][product] for r in records if r['mode']==mode and product in r['products']]
        assert any(c['gap']==c['label_width']+2 and c['version'] is not None for c in candidates),(mode,product,'exact fit not observed')
        assert any(c['gap']==c['label_width']+1 and c['version'] is None for c in candidates),(mode,product,'one cell too narrow not observed')
(out/'proof.json').write_text(json.dumps({'pass':True,'cases':records},ensure_ascii=False,indent=2)+'\n')
