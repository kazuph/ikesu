package render

import (
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
)

func TestParityDesktopBottomBarHasNoExtraStatusRow(t *testing.T) {
	s := testState(160, 45)
	area := s.PanesArea()
	if area.Y != 1 || area.H != 43 || s.SidebarArea().H != 45 {
		t.Fatalf("observed desktop geometry: panes=%+v sidebar=%+v", area, s.SidebarArea())
	}
	bar := s.ActionBarArea()
	if bar.Y != 44 || bar.H != 1 || s.StatusArea() != (layout.Rect{}) {
		t.Fatalf("observed bottom row: action=%+v status=%+v", bar, s.StatusArea())
	}
	out := Compose(Input{State: s})
	text := screenText(t, out, 160, 45)
	if strings.Contains(text, "prefix: C-b") || strings.Contains(text, "[split │]") {
		t.Fatal("desktop renders an additional status bar")
	}
	last := strings.Split(strings.TrimSuffix(text, "\n"), "\n")[44]
	if !strings.Contains(last, "COPY") || !strings.Contains(last, "EQUALIZE") {
		t.Fatalf("missing observed action bar on last row: %q", last)
	}
}
