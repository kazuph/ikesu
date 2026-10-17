package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParitySettingsThemePreviewAndReopenTheme(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, "GTX_CONFIG", path)
	if err := os.WriteFile(path, []byte("[theme]\nname='catppuccin'\n[ui.sound]\nenabled=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	a.reloadSidebarConfig()
	a.openSettings()
	a.settingsKey(uv.Key{Code: uv.KeyDown})
	out := render.Compose(render.Input{State: a.st, SidebarConfig: a.sidebarConfig, Settings: a.settings})
	if a.settings.Theme != "catppuccin-latte" || !reflect.DeepEqual(out.Frame.Palette, render.ThemePalette("catppuccin-latte")) {
		t.Fatal("selected theme must preview")
	}
	cfg, err := config.Load()
	if err != nil || cfg.ThemeName() != "catppuccin" {
		t.Fatal("preview must not save", cfg, err)
	}
	a.settingsKey(uv.Key{Code: uv.KeyRight})
	if a.settings.Theme != "catppuccin-latte" {
		t.Fatal("preview survives section change")
	}
	a.settingsKey(uv.Key{Code: uv.KeyEnter})
	cfg, err = config.Load()
	if err != nil || cfg.ThemeName() != "catppuccin" {
		t.Fatal("sound apply must not save previewed theme", cfg, err)
	}
	a.openSettings()
	if a.settings.Section != 0 {
		t.Fatal("0.2.57 reopens the theme section")
	}
	if a.settings.Theme != "catppuccin" || a.settings.Selected != 0 {
		t.Fatal("reopen must reload saved theme")
	}
	a.settingsKey(uv.Key{Code: uv.KeyDown})
	a.settingsKey(uv.Key{Code: uv.KeyEscape})
	cfg, err = config.Load()
	if err != nil || cfg.ThemeName() != "catppuccin" {
		t.Fatal("cancel must not persist preview", cfg, err)
	}
}
