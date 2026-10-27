#!/bin/sh
set -eu
export PATH="$HOME/bench-terrace.noindex/tools/go/bin:$PATH"
r="$HOME/bench-terrace.noindex/work/detail";out="$1"
go build -o "$r/ikesu-stage5-review" ./cmd/ikesu
python3 tests/parity/stage5_review_cases.py "$out/procedures"
python3 - "$r/user-config.toml" "$out/title-config.toml" <<'PYCONFIG'
import re,sys
s=open(sys.argv[1]).read()
s=re.sub(r'(?m)^mobile_width_threshold\s*=.*$', 'mobile_width_threshold = 0', s)
if not re.search(r'(?m)^mobile_width_threshold\s*=',s):
 s=s.replace('[ui]', '[ui]\nmobile_width_threshold = 0',1)
open(sys.argv[2],'w').write(s)
PYCONFIG
for file in "$out"/procedures/*.steps; do
 name=$(basename "$file" .steps);extra=""
 if [ "$name" = failure ]; then extra="--close-workspace-failure-fixture"; fi
 python3 tests/parity/observe.py --herdr "$r/herdr-0.2.57" --ikesu "$r/ikesu-stage5-review" --ptyrun "$r/ptyrun" --config-file "$out/title-config.toml" --steps "$file" --out "$out/$name" --clipboard-sandbox --server --server-reuse-initial $extra
done
