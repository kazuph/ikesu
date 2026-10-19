package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

func writeReloadConfig(t *testing.T, path, color, prefix string, width int, chrome bool) {
	t.Helper()
	text := fmt.Sprintf("onboarding=false\n[session]\nresume_agents_on_restore=false\n[theme]\nname='dracula'\n[theme.custom]\npanel_bg='%s'\nsurface_dim='%s'\n[keys]\nprefix='%s'\n[ui]\nsidebar_width=%d\nsidebar_start_collapsed=true\nshow_tab_bar=%t\npane_borders=%t\n[ui.sound]\nenabled=false\n", color, color, prefix, width, chrome, chrome)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func reloadCellColor(s *session, want uint32) bool {
	cell := s.emu.CellAt(0, 0)
	if cell == nil || cell.Style.Bg == nil {
		return false
	}
	r, g, b, _ := cell.Style.Bg.RGBA()
	return (r>>8)<<16|(g>>8)<<8|b>>8 == want
}

func TestReloadConfigAllTerminalsAndEntryPointsKeepPane(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeReloadConfig(t, path, "#123456", "ctrl+b", 26, true)
	first := startEnv(t, map[string]string{"GTX_CONFIG": path})
	first.waitFor("first prompt", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	// Opening the sidebar is a viewer preference; startup-only collapse must not reapply.
	first.send("\x02b\x02b")
	first.waitFor("expanded sidebar", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "spaces") })
	second := startEnv(t, map[string]string{"GTX_STATE_DIR": first.stateDir, "GTX_CONFIG": path})
	second.waitFor("second prompt", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	second.waitFor("second expanded sidebar", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "spaces") })
	before, err := first.cli("pane", "get", "p1")
	if err != nil {
		t.Fatal(before, err)
	}
	var response api.Response
	var pane api.PaneInfoResult
	if err := json.Unmarshal([]byte(before), &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.Result, &pane); err != nil {
		t.Fatal(err)
	}
	terminal := pane.Pane.TerminalID
	for _, phase := range []struct {
		label, color, prefix string
		width                int
		rgb                  uint32
		chrome               bool
	}{
		{"cli", "#654321", "ctrl+a", 38, 0x654321, false},
		{"key", "#234567", "ctrl+b", 30, 0x234567, true},
		{"menu", "#345678", "ctrl+a", 34, 0x345678, false},
	} {
		writeReloadConfig(t, path, phase.color, phase.prefix, phase.width, phase.chrome)
		switch phase.label {
		case "cli":
			raw, err := first.cli("server", "reload-config")
			if err != nil || !strings.Contains(raw, `"status":"applied"`) {
				t.Fatal(raw, err)
			}
		case "key":
			first.send("\x01R")
		case "menu":
			x, y := first.find("[menu]")
			if x < 0 {
				t.Fatal(first.text())
			}
			first.click(x, y)
			first.waitFor("global menu", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "reload config") })
			x, y = first.find("reload config")
			first.click(x, y)
		}
		for _, view := range []*session{first, second} {
			view.waitFor(phase.label+" palette and expanded sidebar", 5*time.Second, func(tx string) bool {
				return strings.Contains(tx, "spaces") && reloadCellColor(view, phase.rgb) && view.emu.CellAt(phase.width-2, 1).Content == "─" && view.emu.CellAt(phase.width-1, 1).Content != "─"
			})
			if strings.Contains(view.text(), "%1 terminal") == !phase.chrome {
				t.Fatal("pane border setting not applied", view.text())
			}
			prefix := "\x02"
			if phase.prefix == "ctrl+a" {
				prefix = "\x01"
			}
			view.send(prefix + "?")
			view.waitFor("reloaded prefix", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "keybinds") && strings.Contains(tx, phase.prefix) })
			view.send("\x1b")
			view.waitFor("help closed", 5*time.Second, func(tx string) bool { return !strings.Contains(tx, "keybinds") })
		}
	}
	if err := os.WriteFile(path, []byte("[ui\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := first.cli("server", "reload-config")
	if err != nil || !strings.Contains(raw, `"status":"failed"`) || !strings.Contains(raw, "keeping current config") {
		t.Fatal(raw, err)
	}
	for _, view := range []*session{first, second} {
		view.waitFor("parse warning", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "config parse error") })
		if !reloadCellColor(view, 0x345678) {
			t.Fatal("failed reload discarded palette")
		}
	}
	first.send("printf '\\nLIVE_%s\\n\\n\\n' RELOAD_OK\r")
	first.waitFor("live pane input after failed reload", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "LIVE_RELOAD_OK") })
	after, err := first.cli("pane", "get", "p1")
	if err != nil {
		t.Fatal(after, err)
	}
	if err := json.Unmarshal([]byte(after), &response); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response.Result, &pane); err != nil {
		t.Fatal(err)
	}
	if pane.Pane.TerminalID != terminal {
		t.Fatal("reload replaced pane", terminal, pane.Pane.TerminalID)
	}
}

func TestReloadConfigKeepsEachClientConfigurationOwner(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "first.toml")
	secondPath := filepath.Join(t.TempDir(), "second.toml")
	writeReloadConfig(t, firstPath, "#123456", "ctrl+b", 26, true)
	writeReloadConfig(t, secondPath, "#654321", "ctrl+a", 30, true)
	first := startEnv(t, map[string]string{"GTX_CONFIG": firstPath})
	first.waitFor("first prompt", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	second := startEnv(t, map[string]string{"GTX_STATE_DIR": first.stateDir, "GTX_CONFIG": secondPath})
	second.waitFor("second prompt", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	writeReloadConfig(t, firstPath, "#234567", "ctrl+b", 34, false)
	writeReloadConfig(t, secondPath, "#345678", "ctrl+a", 38, false)
	raw, err := first.cli("server", "reload-config")
	if err != nil {
		t.Fatal(raw, err)
	}
	first.waitFor("first-owned palette", 5*time.Second, func(string) bool { return reloadCellColor(first, 0x234567) })
	second.waitFor("second-owned palette", 5*time.Second, func(string) bool { return reloadCellColor(second, 0x345678) })
}
