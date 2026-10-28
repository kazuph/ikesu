package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginBinPathHomebrewStablePrecedence(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "Cellar", "ikesu", "1.2.3", "bin", "ikesu")
	opt := filepath.Join(root, "opt", "ikesu", "bin", "ikesu")
	bin := filepath.Join(root, "bin", "ikesu")
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
	for _, path := range []string{"/usr/local/bin/ikesu", filepath.Join(root, "Cellar", "bad", "bin", "ikesu"), "relative/ikesu"} {
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
		if v == "IKESU_BIN_PATH="+opt {
			found = true
		}
	}
	if !found {
		t.Fatal(env)
	}
}
