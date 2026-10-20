package config

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReloadedPreservesUIAndDisablesInvalidBinding(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, EnvVar, path)
	previous, err := Decode([]byte("[ui]\nsidebar_width=38\nshow_tab_bar=false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ui]\nsidebar_width='bad'\nshow_tab_bar=true\n[keys]\nnew_tab='prefix+zz'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadReloaded(previous)
	if err != nil || c.ShowTabBar() || *c.UI.SidebarWidth != 38 {
		t.Fatal(c, err)
	}
	if b := c.ActionBindings("new_tab"); len(b) != 1 || b[0] != "" {
		t.Fatal(b)
	}
	if !strings.Contains(c.Warning(), "disabling binding") || !strings.Contains(c.Warning(), "keeping current ui settings") {
		t.Fatal(c.Warnings)
	}
	// The observed 0.2.57 startup also disables an invalid binding.
	start, err := Load()
	if err != nil || !start.ShowTabBar() || start.ActionBindings("new_tab")[0] != "" {
		t.Fatal(start, err)
	}
}
