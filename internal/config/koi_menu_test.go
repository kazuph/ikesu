package config

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKoiMenuEditSavesAndReloadsWithoutLosingOtherValues(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, EnvVar, path)
	if err := os.WriteFile(path, []byte("[ui]\nworkspace_panel_density='slim'\n[extra]\nkeep='value'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true, false} {
		c, err := (Edit{Kind: "koi", Enabled: on}).Apply()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load()
		if err != nil || c.KoiEnabled() != on || loaded.KoiEnabled() != on {
			t.Fatal("saved koi diverges from reload", on, err)
		}
		bytes, _ := os.ReadFile(path)
		if !strings.Contains(string(bytes), "value") || !strings.Contains(string(bytes), "slim") {
			t.Fatal("unrelated config was lost")
		}
	}
}
