#!/usr/bin/env python3
import json
from pathlib import Path
import sys

root=Path(sys.argv[1]);proof={}
for product,label in [('ikesu','ikesu v0.1.0'),('herdr','herdr v0.2.61')]:
    def row(name):
        doc=json.loads((root/product/(name+'.json')).read_text())
        return ''.join(c['Content'] or ' ' for c in doc['Cells'][-1])
    before,notice,restored=[row(n) for n in ['before','notice','restored']]
    assert label in before and label in restored,(product,'version not restored',restored)
    left=before.index(' COPY ')+len(' COPY ');right=before.index(' CYCLE LAYOUT ')
    message=notice[left:right].strip()
    assert label not in notice and 'Footer offline' in message,(product,'connection notice missing',notice)
    assert any(word in message.lower() for word in ['permission denied','host key','authentication failed']),(product,message)
    for button in [' COPY ',' CYCLE LAYOUT ',' ROTATE PANES ',' EQUALIZE ']:
        assert before.index(button)==notice.index(button)==restored.index(button),(product,button)
    proof[product]={'notice':message,'buttons_unchanged':True,'version_restored':True}
(root/'proof.json').write_text(json.dumps({'pass':True,'products':proof},ensure_ascii=False,indent=2)+'\n')
