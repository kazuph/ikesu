package tui

import (
	"fmt"
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
	if a.dialog != nil || a.settings != nil || a.workspaceNavigate {
		t.Fatal("continue did not return to normal mode")
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

func TestConfigOnboardingImmediateInputReachesPane(t *testing.T) {
	teststate.Lock(t)
	for _, server := range []bool{false, true} {
		for _, key := range []string{"\r", "l", "\x1b[C"} {
			t.Run(fmt.Sprintf("server=%v/key=%q", server, key), func(t *testing.T) {
				a := specRuntimeApp(t)
				path := filepath.Join(t.TempDir(), "config.toml")
				teststate.Setenv(t, config.EnvVar, path)
				if err := os.WriteFile(path, []byte("onboarding=true\n"), 0600); err != nil {
					t.Fatal(err)
				}
				c, err := config.Load()
				if err != nil {
					t.Fatal(err)
				}
				if server {
					a.SetServerUI()
				}
				a.SetSidebarConfig(c, true)
				p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec /bin/cat"})
				if err != nil {
					t.Fatal(err)
				}
				specWaitText(t, p, "READY")
				a.StartOnboarding()
				specFeed(a, key+"resize_input\r")
				if a.dialog != nil || a.workspaceNavigate || a.resizeMode {
					t.Fatal("continue consumed subsequent text as operations")
				}
				specWaitText(t, p, "resize_input")
			})
		}
	}
}
func TestConfigOnboardingOtherInputKeepsWelcome(t *testing.T) {
	teststate.Lock(t)
	for _, key := range []string{"a", "r", " ", "\x1b"} {
		t.Run(fmt.Sprintf("key=%q", key), func(t *testing.T) {
			a := specRuntimeApp(t)
			path := filepath.Join(t.TempDir(), "config.toml")
			teststate.Setenv(t, config.EnvVar, path)
			if err := os.WriteFile(path, []byte("onboarding=true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			a.SetSidebarConfig(c, false)
			a.StartOnboarding()
			specFeed(a, key)
			if a.dialog == nil || !a.dialog.onboarding || a.workspaceNavigate || a.resizeMode {
				t.Fatal("ignored input changed welcome or mode")
			}
			c, err = config.Load()
			if err != nil || c.Onboarding == nil || !*c.Onboarding {
				t.Fatal(c, err)
			}
		})
	}
}
