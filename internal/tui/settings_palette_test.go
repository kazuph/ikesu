package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestSettingsApplyKeepsEndpointPaneThemeUntilConfigReload(t *testing.T) {
	teststate.Lock(t)
	for _, endpoint := range []bool{false, true} {
		t.Run(fmt.Sprint(endpoint), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			teststate.Setenv(t, "IKESU_CONFIG", path)
			if err := os.WriteFile(path, []byte("[theme]\nname='tokyo-night'\n[theme.custom]\npanel_bg='transparent'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			a := specRuntimeApp(t)
			if endpoint {
				a.SetServerUI()
			}
			a.reloadSidebarConfig()
			old := *a.panePalette
			a.openSettings()
			for a.settings.Theme != "dracula" {
				a.settingsKey(uv.Key{Code: uv.KeyUp})
				if a.settings.Selected == 0 {
					break
				}
			}
			for a.settings.Theme != "dracula" {
				a.settingsKey(uv.Key{Code: uv.KeyDown})
				if a.settings.Selected == len(render.ThemeNames)-1 {
					t.Fatal("dracula missing from configured theme list")
				}
			}
			a.settingsKey(uv.Key{Code: uv.KeyEnter})
			cfg, err := config.Load()
			if err != nil || cfg.ThemeName() != "dracula" || a.sidebarConfig.ThemeName() != "dracula" {
				t.Fatal("apply did not persist the new UI theme", cfg, err)
			}
			want := render.CustomPalette("dracula", cfg.Theme.Custom)
			border := func() uv.Style {
				f := render.Compose(render.Input{State: a.st, ServerUI: endpoint, SidebarConfig: a.sidebarConfig, PanePalette: a.panePalette}).Frame
				area := a.st.PanesArea()
				return f.Cell(area.X, area.Y).Style
			}
			color := want.Accent
			if endpoint {
				color = old.Accent
			}
			if border().Fg != color {
				t.Fatal("apply changed the pane at the wrong time", border(), color)
			}
			a.reloadSidebarConfig()
			if border().Fg != want.Accent {
				t.Fatal("reload did not update the pane theme", border(), want.Accent)
			}
		})
	}
}
