package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestReloadCustomThemeResetsOnlyInvalidValue(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	a := specRuntimeApp(t)
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		a.reloadSidebarConfig()
	}
	write("[theme]\nname='tokyo-night'\n[theme.custom]\npanel_bg='transparent'\n[ui]\ntransparent_sidebar_cards=true\n")
	frame := render.Compose(render.Input{State: a.st, SidebarConfig: a.sidebarConfig}).Frame
	if frame.Palette.PopupBG != nil || !a.sidebarConfig.UI.TransparentSidebarCards {
		t.Fatal("reload did not apply transparency")
	}
	write("[theme]\nname='tokyo-night'\n[theme.custom]\npanel_bg='invalid-color'\n[ui]\ntransparent_sidebar_cards=true\n")
	if a.sidebarConfig.ThemeName() != "tokyo-night" || !a.sidebarConfig.UI.TransparentSidebarCards {
		t.Fatal("invalid value discarded valid siblings")
	}
	if a.sidebarConfig.Theme.Custom["panel_bg"] != "" || a.sidebarConfig.Warning() == "" {
		t.Fatal("invalid color did not reset with warning")
	}
	write("[theme]\nname='dracula'\n")
	if a.sidebarConfig.UI.TransparentSidebarCards || render.CustomPalette(a.sidebarConfig.ThemeName(), a.sidebarConfig.Theme.Custom).PopupBG == nil || frame.Palette.PopupBG != nil {
		t.Fatal("reload did not remove overrides, or mutated an earlier frame")
	}
}
