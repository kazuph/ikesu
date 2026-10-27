package tui

import (
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestSudoDisplayPriorityAndUnderlyingAPIStatus(t *testing.T) {
	for _, key := range []string{"working", "done", "idle", "unknown", "blocked"} {
		g := paneStatusGlyph(key, 0, render.ThemePalette("catppuccin"), true)
		if g.Key != key {
			t.Fatal("API status changed", key, g)
		}
		if key == "blocked" {
			if g.Color != render.Red || g.Rune != "●" {
				t.Fatal("blocked must override sudo", g)
			}
		} else if g.Color != render.Mauve || g.Rune != "◆" {
			t.Fatal("sudo did not override underlying state", key, g)
		}
		original := paneStatusGlyph(key, 0, render.ThemePalette("catppuccin"), false)
		if original.Key != key || original.Rune == "◆" {
			t.Fatal("underlying state not restored", original)
		}
	}
}

func TestSudoDisplayDoesNotAddNavigatorFilterOrAPIStatusName(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"working", "done", "idle", "unknown", "blocked"} {
		a.SetReported(1, "codex", key)
		if a.PaneStatusKey(1) != key {
			t.Fatal("API changed", key)
		}
		a.navigator = &navigatorView{filter: key}
		rows, _ := a.navigatorRows()
		found := false
		for _, row := range rows {
			if row.Pane == 1 {
				found = true
			}
		}
		if !found {
			t.Fatal("existing filter lost pane", key)
		}
	}
}
