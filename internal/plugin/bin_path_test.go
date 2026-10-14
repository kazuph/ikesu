package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginBinPathHomebrewStablePrecedence(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "Cellar", "goterrace", "1.2.3", "bin", "gtx")
	opt := filepath.Join(root, "opt", "goterrace", "bin", "gtx")
	bin := filepath.Join(root, "bin", "gtx")
	if got := stableBinPath(original); got != original {
		t.Fatal(got)
	}
	for _, path := range []string{bin, opt} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("binary"), 0700); err != nil {
			t.Fatal(err)
		}
		if got := stableBinPath(original); got != path {
			t.Fatal(got, path)
		}
	}
	for _, path := range []string{"/usr/local/bin/gtx", filepath.Join(root, "Cellar", "bad", "bin", "gtx"), "relative/gtx"} {
		if got := stableBinPath(path); got != path {
			t.Fatal(got, path)
		}
	}
	engine, installed := actionEngine(t, minimal)
	engine.BinPath = original
	env, err := engine.Environment(installed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range env {
		if v == "GTX_BIN_PATH="+opt {
			found = true
		}
	}
	if !found {
		t.Fatal(env)
	}
}
