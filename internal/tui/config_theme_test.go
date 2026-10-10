package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigThemeAutoSwitchAndManualPersistence(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	if err := os.WriteFile(path, []byte("onboarding=false\n[theme]\nname='tokyo-night'\nauto_switch=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	specFeed(a, "\x1b[?997;2n")
	if a.sidebarConfig.ThemeName() != "tokyo-night-day" {
		t.Fatal(a.sidebarConfig.ThemeName())
	}
	specFeed(a, "\x1b[?997;1n")
	if a.sidebarConfig.ThemeName() != "tokyo-night" {
		t.Fatal(a.sidebarConfig.ThemeName())
	}
	if err := config.SaveTheme("dracula"); err != nil {
		t.Fatal(err)
	}
	c, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	specFeed(a, "\x1b[?997;2n")
	if a.sidebarConfig.ThemeName() != "dracula" || a.sidebarConfig.Theme.AutoSwitch {
		t.Fatal("manual theme changed")
	}
}
func TestConfigOnboardingContinuePreservesConfig(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	if err := os.WriteFile(path, []byte("onboarding=true\n[theme]\nname='tokyo-night'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(c, false)
	a.StartOnboarding()
	if a.dialog == nil || !a.dialog.onboarding {
		t.Fatal("missing welcome")
	}
	specFeed(a, "l")
	if a.dialog != nil || a.settings != nil || !a.workspaceNavigate {
		t.Fatal("continue did not return to navigation")
	}
	c, err = config.Load()
	if err != nil || c.Onboarding == nil || *c.Onboarding || c.ThemeName() != "tokyo-night" {
		t.Fatal(c, err)
	}
	a.StartOnboarding()
	if a.dialog != nil {
		t.Fatal("welcome repeated")
	}
}
