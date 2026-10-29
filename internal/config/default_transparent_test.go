package config

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestBackgroundDefaultWithMissingConfiguration(t *testing.T) {
	teststate.Lock(t)
	home := t.TempDir()
	teststate.Setenv(t, "HOME", home)
	teststate.Setenv(t, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	teststate.Setenv(t, EnvVar, "")
	c, err := Load()
	if err != nil || c.UI.OpaqueBackground {
		t.Fatal("missing configuration must use transparent background", c, err)
	}
	if _, err := os.Stat(Path()); !os.IsNotExist(err) {
		t.Fatal("read of missing configuration created a file", err)
	}
	for _, value := range []bool{false, true} {
		input := "[ui]\nopaque_background = false\n"
		if value {
			input = "[ui]\nopaque_background = true\n"
		}
		c, err := Decode([]byte(input))
		if err != nil || len(c.Warnings) != 0 || c.UI.OpaqueBackground != value {
			t.Fatal(c, err)
		}
	}
}
