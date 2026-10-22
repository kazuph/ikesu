package plugin

import (
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/guard"
)

func TestActionConfigurationPathIsActualReadPath(t *testing.T) {
	e, p := actionEngine(t, minimal)
	for _, override := range []string{"", filepath.Join(t.TempDir(), "custom.toml")} {
		t.Setenv("IKESU_CONFIG", override)
		t.Setenv("IKESU_CONFIG_PATH", "/inherited/outer.toml")
		env, err := e.Environment(p, Context{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := guard.EnvMap(env)["IKESU_CONFIG_PATH"]; got != config.Path() {
			t.Fatalf("got %q want %q", got, config.Path())
		}
	}
}
