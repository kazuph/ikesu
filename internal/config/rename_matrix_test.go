package config

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageOverridesAcrossExecutableNames(t *testing.T) {
	for _, name := range []string{"ikesu", "ikesu-xyz", "foo"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("IKESU_CONFIG", "")
			t.Setenv("IKESU_STATE_DIR", "")
			t.Setenv("IKESU_DB_PATH", "")
			api.ConfigureExecutable(name)
			if api.StateDir() != filepath.Join(home, ".local/state/ikesu") {
				t.Fatal(api.StateDir())
			}
			if Path() != filepath.Join(home, ".config/ikesu/config.toml") {
				t.Fatal(Path())
			}
			if dispatch.DBPath() != filepath.Join(api.StateDir(), "ikesu.db") {
				t.Fatal(dispatch.DBPath())
			}
			root := filepath.Join(home, "custom")
			t.Setenv("IKESU_STATE_DIR", root)
			t.Setenv("IKESU_DB_PATH", filepath.Join(home, "explicit.db"))
			if api.StateDir() != root || dispatch.DBPath() != filepath.Join(home, "explicit.db") {
				t.Fatal("override lost")
			}
			store, err := dispatch.Open(dispatch.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(dispatch.DBPath()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
