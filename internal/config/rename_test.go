package config

import (
	"github.com/kazuph/ikesu/internal/api"
	"path/filepath"
	"testing"
)

func TestStageOneConfigPaths(t *testing.T) {
	t.Setenv("GTX_CONFIG", "")
	t.Setenv("IKESU_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"ikesu", "gtx", "gtx-parity"} {
		api.ConfigureExecutable(name)
		ns := name
		if name == "ikesu" {
			ns = "gtx"
		}
		// Both commands must read and write the same config file.
		root := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", root)
		if got := Path(); got != filepath.Join(root, ns, "config.toml") {
			t.Fatalf("%s path=%s", name, got)
		}
	}
	api.ConfigureExecutable("gtx")
	t.Cleanup(func() { api.ConfigureExecutable("gtx") })
	t.Setenv("GTX_CONFIG", "/old/config.toml")
	t.Setenv("IKESU_CONFIG", "/new/config.toml")
	if Path() != "/new/config.toml" {
		t.Fatal(Path())
	}
	t.Setenv("IKESU_CONFIG", "")
	if Path() != "/old/config.toml" {
		t.Fatal(Path())
	}
}
