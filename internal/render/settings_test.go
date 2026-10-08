package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"strings"
	"testing"
)

func TestG2SettingsReferenceGeometryOrderAndExcludedSections(t *testing.T) {
	s := model.New(t.TempDir(), 120, 50)
	view := &Settings{Theme: "catppuccin", Sound: true, Toast: "off"}
	for section := 0; section < 3; section++ {
		view.Section = section
		f := Compose(Input{State: s, Settings: view}).Frame
		line := func(y int) string {
			out := ""
			for x := 23; x < 97; x++ {
				out += f.Cell(x, y).Content
			}
			return strings.TrimSpace(out)
		}
		if f.Cell(22, 14).Content != "┌" || f.Cell(97, 35).Content != "┘" || line(15) != "settings" || line(16) != "theme   sound   toasts" || line(33) != "↑↓ select  tab section" {
			t.Fatal("reference frame", section, line(16))
		}
		switch section {
		case 0:
			if line(19) != "▸ catppuccin ✓" || line(31) != "solarized-light" {
				t.Fatal("theme rows")
			}
		case 1:
			if line(22) != "sound alerts: on ✓" || line(23) != "sound alerts: off" {
				t.Fatal("sound rows")
			}
		case 2:
			if line(22) != "notification popups: off ✓" || line(24) != "notification popups: inside gtx" || line(28) != "notification popups: via system" {
				t.Fatal("toast rows")
			}
		}
		for y := 14; y <= 35; y++ {
			if strings.Contains(line(y), "integrations") || strings.Contains(line(y), "pane label") {
				t.Fatal("excluded settings")
			}
		}
	}
}
