package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityCopyWideWordAndLineEndFromPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 60, 12, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 30 ]; do printf '日本語 alpha.beta \\033[31mRED\\033[0m あいう\\n'; i=$((i+1)); done; printf DONE; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "DONE")
	a.enterCopy(false)
	specFeed(a, "g0")
	for _, step := range []struct {
		key string
		col int
	}{
		{"e", 4}, {"w", 7}, {"b", 0}, {"$", 26}, {"w", 0}, {"b", 22},
	} {
		specFeed(a, step.key)
		if a.cm.cx != step.col {
			t.Fatalf("%q column=%d want=%d", step.key, a.cm.cx, step.col)
		}
	}
}
