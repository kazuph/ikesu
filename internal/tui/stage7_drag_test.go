package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/teststate"
	"io"
	"testing"
	"time"
)

func TestStage7DragEdgeAutoScrollAndClickControl(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	pid := a.cm.pane
	a.exitCopy()
	inner := a.innerRect(pid)
	a.sel = &selState{pane: pid, ax: 5, ay: viewTop(a.sup.Get(pid), 0, inner.H), inner: inner}
	a.selMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 5, Y: inner.Y, Button: uv.MouseLeft}})
	a.selectionAutoScroll(time.Now().Add(time.Second))
	if a.scroll[pid] != 0 {
		t.Fatal("edge click scrolled")
	}
	a.selMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: inner.X + 5, Y: inner.Y - 2, Button: uv.MouseLeft}})
	if a.scroll[pid] != 4 {
		t.Fatalf("outside distance: offset=%d want 4", a.scroll[pid])
	}
	at := a.sel.autoAt
	a.selectionAutoScroll(at.Add(3 * selectionScrollInterval))
	if a.scroll[pid] != 7 {
		t.Fatalf("stationary drag offset=%d want 7", a.scroll[pid])
	}
	a.selMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: inner.X + 5, Y: inner.Y + inner.H - 1, Button: uv.MouseLeft}})
	at = a.sel.autoAt
	a.selectionAutoScroll(at.Add(2 * selectionScrollInterval))
	if a.scroll[pid] != 5 {
		t.Fatalf("bottom edge offset=%d want 5", a.scroll[pid])
	}
	a.sel = nil
	a.selectionAutoScroll(at.Add(time.Second))
	if a.scroll[pid] != 5 {
		t.Fatal("selection release did not stop scroll")
	}
}

func TestStage7ViewerDragReceivesTicks(t *testing.T) {
	teststate.Lock(t)
	a := stage7CopyApp(t)
	pid := a.cm.pane
	a.exitCopy()
	v := a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
	inner := v.app.innerRect(pid)
	v.app.sel = &selState{pane: pid, inner: inner, moved: true, autoDir: 1, autoAt: time.Now().Add(-3 * selectionScrollInterval)}
	v.Tick(a)
	if v.app.scroll[pid] < 3 {
		t.Fatal("endpoint did not tick its selection")
	}
	if a.scroll[pid] != 0 {
		t.Fatal("viewer changed session scroll")
	}
}
