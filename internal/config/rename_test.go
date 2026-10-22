package config

import (
	"github.com/kazuph/ikesu/internal/api"
	"path/filepath"
	"testing"
)

func TestExecutableIndependentConfig(t *testing.T) {
	t.Setenv("IKESU_CONFIG", "")
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	for _, name := range []string{"ikesu", "ikesu-xyz", "foo"} {
		api.ConfigureExecutable(name)
		if got := Path(); got != filepath.Join(root, "ikesu", "config.toml") {
			t.Fatal(name, got)
		}
	}
	t.Setenv("IKESU_CONFIG", "/explicit/config.toml")
	if Path() != "/explicit/config.toml" {
		t.Fatal(Path())
	}
}
