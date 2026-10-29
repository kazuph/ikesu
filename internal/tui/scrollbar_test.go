package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"testing"
	"time"
)

func TestPaneScrollbarExpiryDragAndViewerIsolation(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), io.Discard, "")
	pid, now := a.focusedPane(), time.Now()
	if a.paneScrollbarVisible(pid, now) {
		t.Fatal("auto visible without interaction")
	}
	a.SetScrollOffset(pid, 3)
	until := a.scrollbarUntil[pid]
	if !a.paneScrollbarVisible(pid, until.Add(-time.Nanosecond)) || a.paneScrollbarVisible(pid, until) {
		t.Fatal("expiry boundary")
	}
	a.sb = &sbDrag{pane: pid}
	a.expirePaneScrollbars(until)
	if !a.paneScrollbarVisible(pid, until) {
		t.Fatal("active drag expired")
	}
	a.sbMouse(input.Event{Kind: input.KindMouseRelease})
	if !a.paneScrollbarVisible(pid, time.Now()) {
		t.Fatal("release did not restart interval")
	}
	v := a.NewViewer(io.Discard, 160, 30, nil)
	if v.app.paneScrollbarVisible(pid, time.Now()) {
		t.Fatal("viewer inherited interaction")
	}
	a.dirty = false
	a.expirePaneScrollbars(a.scrollbarUntil[pid])
	if !a.dirty || len(a.scrollbarUntil) != 0 {
		t.Fatal("expiry did not repaint")
	}
	a.sidebarConfig.UI.PaneScrollbars = "always"
	if !a.paneScrollbarVisible(pid, now) {
		t.Fatal("always hidden")
	}
	a.sidebarConfig.UI.PaneScrollbars = "off"
	a.showPaneScrollbar(pid)
	if a.paneScrollbarVisible(pid, now) {
		t.Fatal("off visible")
	}
}

func TestPaneScrollbarRealPTYWheelCopyAndWidth(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	r := a.innerRect(pid)
	p, err := a.sup.SpawnWith(pid, a.cwd, r.W, r.H, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 150 ]; do echo LINE-$i; i=$((i+1)); done; printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	if a.paneScrollbarVisible(pid, time.Now()) {
		t.Fatal("output exposed auto scrollbar")
	}
	a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: r.X + 2, Y: r.Y + 2, Button: uv.MouseWheelUp}})
	if a.scroll[pid] != 3 || !a.paneScrollbarVisible(pid, time.Now()) {
		t.Fatal("wheel did not show scrollbar")
	}
	a.expirePaneScrollbars(a.scrollbarUntil[pid])
	a.enterCopy(false)
	a.copyKey(uv.Key{Code: uv.KeyUp}, nil)
	if !a.paneScrollbarVisible(pid, time.Now()) {
		t.Fatal("copy movement did not show scrollbar")
	}
	a.draw()
	a.expirePaneScrollbars(a.scrollbarUntil[pid])
	a.draw()
	p.WithEmu(func(e *vt.Emulator) {
		if e.Width() != r.W {
			t.Fatalf("width changed: %d want %d", e.Width(), r.W)
		}
	})
	a.sidebarConfig = config.Config{}
}
