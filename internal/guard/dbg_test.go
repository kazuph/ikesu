package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDbg(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, "work/shorts"), 0700)
	os.WriteFile(filepath.Join(d, "work/shorts/render.sh"), []byte("/bin/rm /outside/x\n"), 0600)
	i := Input{Cwd: d, LaunchCwd: d, Env: map[string]string{"PATH": "/usr/bin:/bin", "HOME": d}}
	for _, l := range []string{`cd work/shorts && /bin/bash render.sh`, `cd work/shorts && printf prep > prep.txt && /bin/bash render.sh 2>&1 | grep -E "ran|Error" && printf done > /tmp/after`} {
		_, v, e := lineVerdict(Config{}, i, l)
		t.Logf("%s => %+v %v", l, v.Decision+"/"+v.Rule+"/"+v.Reason, e)
	}
}
