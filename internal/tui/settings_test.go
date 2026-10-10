package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestG2SettingsSectionsSaveApplyReloadAndClose(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, "GTX_CONFIG", path)
	if err := os.WriteFile(path, []byte("[ui]\nworkspace_panel_density='slim'\n[ui.toast]\ndelivery='off'\n[extra]\nkeep='value'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	a.exec(model.CmdResize{Cols: 120, Rows: 50})
	a.reloadSidebarConfig()
	applied := config.Config{}
	a.SetConfigChanged(func(c config.Config) { applied = c })
	a.openMainMenu(0, 0)
	a.activateMenu(3)
	if a.settings == nil || a.settings.Section != 0 {
		t.Fatal("settings menu")
	}
	specFeed(a, "\x1b[B\r")
	if a.settings != nil || a.sidebarConfig.ThemeName() != "catppuccin-latte" || applied.ThemeName() != "catppuccin-latte" {
		t.Fatal("theme apply", a.settings)
	}
	a.openSettings()
	specFeed(a, "\t\x1b[B\r")
	if a.settings != nil || a.sidebarConfig.SoundEnabled() {
		t.Fatal("sound save")
	}
	a.openSettings()
	if a.settings.Section != 1 {
		t.Fatal("reopen must retain sound section")
	}
	specFeed(a, "\t\x1b[B\r")
	if a.settings != nil || a.sidebarConfig.ToastDelivery() != config.ToastGTX || applied.ToastDelivery() != config.ToastGTX {
		t.Fatal("toast apply")
	}
	a.openSettings()
	a.settingsKey(uv.Key{Code: uv.KeyEscape})
	if a.settings != nil {
		t.Fatal("escape close")
	}
	a.openSettings()
	if a.settings.Theme != "catppuccin-latte" || a.settings.Sound || a.settings.Toast != "gtx" {
		t.Fatal("reopen values")
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "value") || a.st.WorkspaceDensity != "slim" {
		t.Fatal("other config lost", err, string(b))
	}
	r := render.SettingsRect(120, 50)
	sidebarEvent(a, input.KindMouseClick, r.X+20, r.Y+2)
	if a.settings.Section != 2 {
		t.Fatal("tab mouse")
	}
	sidebarEvent(a, input.KindMouseClick, r.X+4, render.SettingsChoiceY(r, 2, 3))
	if a.settings.Selected != 3 || a.sidebarConfig.ToastDelivery() != config.ToastGTX {
		t.Fatal("choice applied before confirmation")
	}
	sidebarEvent(a, input.KindMouseClick, r.X+30, r.Y+r.H-2)
	if a.sidebarConfig.ToastDelivery() != config.ToastSystem {
		t.Fatal("mouse apply")
	}
	a.openSettings()
	sidebarEvent(a, input.KindMouseClick, r.X+43, r.Y+r.H-2)
	if a.settings != nil {
		t.Fatal("close mouse")
	}
}
func TestG2SettingsFailedWriteDoesNotApply(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, "GTX_CONFIG", path)
	if err := os.WriteFile(path, []byte("invalid ["), 0600); err != nil {
		t.Fatal(err)
	}
	a := specRuntimeApp(t)
	a.openSettings()
	a.settings.Selected = 1
	a.settingsApply()
	if a.sidebarConfig.ThemeName() != "catppuccin" || a.settings.Theme != "catppuccin" {
		t.Fatal("failed write applied")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "invalid [" {
		t.Fatal("failed write overwrote config")
	}
}
func TestSettingsShrinkingKeepsSelectedThemeVisibleAndMouseAligned(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.openSettings()
	a.settings.Selected, a.settings.Theme = 3, "tokyo-night"
	for _, height := range []int{12, 13, 14, 15} {
		a.exec(model.CmdResize{Cols: 60, Rows: height})
		r := render.SettingsRect(60, height)
		offset := render.SettingsChoiceOffset(r, a.settings)
		y := render.SettingsChoiceY(r, 0, 3-offset)
		f := render.Compose(render.Input{State: a.st, Settings: a.settings}).Frame
		if f.Cell(r.X+2, y).Content != "▸" {
			t.Fatalf("selected theme missing at height %d", height)
		}
		sidebarEvent(a, input.KindMouseClick, r.X+4, y)
		if a.settings.Selected != 3 || a.settings.Theme != "tokyo-night" {
			t.Fatalf("selected theme click changed target at height %d", height)
		}
	}
	a.exec(model.CmdResize{Cols: 60, Rows: 13})
	r := render.SettingsRect(60, 13)
	sidebarEvent(a, input.KindMouseClick, r.X+4, render.SettingsChoiceY(r, 0, 0))
	if a.settings.Selected != 2 || a.settings.Theme != "terminal" {
		t.Fatal("first visible theme click used the unscrolled index")
	}
}
func TestG8SidebarHasNoUpdateAttentionOrWhatsNew(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.openMainMenu(0, 0)
	for _, item := range a.menu.m.Items {
		if strings.Contains(strings.ToLower(item.Label), "update") || strings.Contains(strings.ToLower(item.Label), "what's new") {
			t.Fatal("disabled update menu", item)
		}
	}
	frame := render.Compose(render.Input{State: a.st}).Frame
	for y := 0; y < frame.H; y++ {
		line := ""
		for x := 0; x < frame.W; x++ {
			line += frame.Cell(x, y).Content
		}
		if strings.Contains(strings.ToLower(line), "update") {
			t.Fatal("update badge", line)
		}
	}
}
