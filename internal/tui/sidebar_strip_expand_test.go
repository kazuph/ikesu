package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"path/filepath"
	"testing"
	"time"
)

func stripApp(t *testing.T, size int) (*App, string) {
	t.Helper()
	a := NewApp(model.New(t.TempDir(), 120, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarVisible = false
	a.st.SidebarSize = size
	path := filepath.Join(t.TempDir(), "preferences.json")
	a.preferenceSave = func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) }
	if a.st.SidebarArea().W != model.SidebarCollapsedWidth {
		t.Fatalf("fixture is not the two-cell strip: %d", a.st.SidebarArea().W)
	}
	return a, path
}

func stripMouse(a *App, kind input.Kind, x, y int) {
	m := uv.Mouse{X: x, Y: y, Button: uv.MouseLeft}
	if kind == input.KindMouseClick {
		a.sidebarClick(m)
		return
	}
	a.sidebarGestureMouseAt(input.Event{Kind: kind, Mouse: m}, time.Now())
}

// The top cell of the strip restores the remembered width and saves it.
func TestCollapsedStripTopCellClickRestoresSidebarAndSaves(t *testing.T) {
	for _, size := range []int{0, 2, 30} {
		a, path := stripApp(t, size)
		want := a.st.ExpandedSidebarWidth()
		stripMouse(a, input.KindMouseClick, 0, 0)
		if a.sidebarGesture == nil {
			t.Fatalf("size %d: press did not start a gesture", size)
		}
		if a.st.SidebarVisible {
			t.Fatalf("size %d: restored on press, before release", size)
		}
		stripMouse(a, input.KindMouseRelease, 0, 0)
		if !a.st.SidebarVisible || a.st.SidebarHidden || a.st.SidebarArea().W != want || a.st.SidebarSize != want {
			t.Fatalf("size %d: visible=%t hidden=%t width=%d size=%d want %d", size, a.st.SidebarVisible, a.st.SidebarHidden, a.st.SidebarArea().W, a.st.SidebarSize, want)
		}
		p, err := endpoint.LoadViewPreferences(path)
		if err != nil || p == nil || p.SidebarCollapsed || p.SidebarHidden || p.SidebarWidth != want {
			t.Fatalf("size %d: saved %+v, %v", size, p, err)
		}
	}
}

// Dragging off the cell, or clicking another strip cell, leaves the strip as it was.
func TestCollapsedStripOtherInputKeepsStrip(t *testing.T) {
	a, _ := stripApp(t, 0)
	stripMouse(a, input.KindMouseClick, 0, 0)
	stripMouse(a, input.KindMouseMotion, 0, 4)
	stripMouse(a, input.KindMouseRelease, 0, 4)
	if a.st.SidebarVisible || a.sidebarGesture != nil {
		t.Fatal("drag off the top cell restored the sidebar")
	}
	for _, y := range []int{1, 3, 6} {
		stripMouse(a, input.KindMouseClick, 0, y)
		a.sidebarGesture = nil
		if a.st.SidebarVisible {
			t.Fatalf("row %d restored the sidebar", y)
		}
	}
	// The right edge still grabs the divider.
	if !a.sidebarPress(uv.Mouse{X: a.st.SidebarArea().W - 1, Y: 5, Button: uv.MouseLeft}) || a.sidebarGesture == nil || !a.sidebarGesture.divider {
		t.Fatal("divider column no longer grabbed")
	}
}
