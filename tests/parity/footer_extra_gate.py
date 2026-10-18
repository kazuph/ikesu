#!/usr/bin/env python3
"""Check notification priority and old client version after a real server handoff."""
import json
from pathlib import Path
import sys

root=Path(sys.argv[1])
proof={}
for product,label in [('gtx','gtx v0.1.0'),('herdr','herdr v0.2.61')]:
    directory=root/product
    def row(name):
        data=json.loads((directory/(name+'.json')).read_text())
        return ''.join(c['Content'] or ' ' for c in data['Cells'][-1])
    before,notice,restored,handoff=[row(name) for name in ['before','notice','restored','handoff']]
    assert label in before and label in restored,(product,'missing before/after version')
    left=before.index(' COPY ')+len(' COPY ');right=before.index(' CYCLE LAYOUT ')
    message=notice[left:right].strip()
    if product=='gtx':
        assert label not in notice and message,(product,'notification did not suppress version',notice)
        assert message=='machine catalog: unexpected end of JSON input',message
        runtime=json.loads((directory/'runtime-json.json').read_text())
        result=json.loads(runtime['home/footer-handoff.json'])
        assert result['before']['result']['version']=='0.1.0',result
        assert result['after']['result']['version']=='0.1.0-footer-test',result
        assert label in handoff and 'gtx v0.1.0-footer-test' not in handoff,handoff
        proof['handoff']=result
    for text in [notice,restored,handoff]:
        for button in [' COPY ',' CYCLE LAYOUT ',' ROTATE PANES ',' EQUALIZE ']:
            assert text.index(button)==before.index(button),(product,'button moved',button)
    proof[product]={'notice':message if product=='gtx' else 'not triggered by invalid catalog; connection notice comparison remains unverified','version_restored':True}
(root/'proof.json').write_text(json.dumps({'pass':True,**proof},ensure_ascii=False,indent=2)+'\n')
