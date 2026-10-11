package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLineLexicalParsingNeverEvaluatesInput(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "must-not-exist")
	i := Input{Cwd: d, Env: map[string]string{"HOME": d, "ZDOTDIR": d, "PATH": "/usr/bin:/bin"}}
	raw := "npx 'two words' $(touch " + target + ") deploy"
	got, e := LineInput(i, raw)
	if e != nil {
		t.Fatal(e)
	}
	if got.Command != "npx" || got.Args[0] != "two words" || got.RawLine != raw {
		t.Fatal(got)
	}
	if _, e = os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("input evaluated", e)
	}
}
