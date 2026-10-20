package tui

import (
	"bytes"
	"encoding/base64"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"strings"
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
	// The observed control releases the edge click, then starts a new drag in the middle.
	a.sel = &selState{pane: pid, ax: 5, ay: viewTop(a.sup.Get(pid), 0, inner.H) + inner.H/2, inner: inner}
	a.selMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: inner.X + 5, Y: inner.Y - 2, Button: uv.MouseLeft}})
	if a.scroll[pid] != 6 {
		t.Fatalf("outside distance: offset=%d want 6", a.scroll[pid])
	}
	at := a.sel.autoAt
	a.selectionAutoScroll(at.Add(3 * selectionScrollInterval))
	if a.scroll[pid] != 9 {
		t.Fatalf("stationary drag offset=%d want 9", a.scroll[pid])
	}
	a.selMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: inner.X + 5, Y: inner.Y + inner.H - 1, Button: uv.MouseLeft}})
	at = a.sel.autoAt
	a.selectionAutoScroll(at.Add(2 * selectionScrollInterval))
	if a.scroll[pid] != 7 {
		t.Fatalf("bottom edge offset=%d want 7", a.scroll[pid])
	}
	a.sel = nil
	a.selectionAutoScroll(at.Add(time.Second))
	if a.scroll[pid] != 7 {
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

func TestStage7DragCopyOmitsFinalBlankRow(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SSH_CONNECTION", "isolated-test")
	a := stage7CopyApp(t)
	pid := a.cm.pane
	a.exitCopy()
	var out bytes.Buffer
	a.out = &out
	a.sel = &selState{pane: pid, ax: 0, ay: 120, hx: 0, hy: 121, moved: true, inner: a.innerRect(pid)}
	a.finishSelection()
	if out.String() != "\x1b]52;c;U1RBR0U3X1JFQURZ\a" {
		t.Fatalf("clipboard included final empty row: %q", out.String())
	}
}

func TestStage7OutsideReleaseKeepsVisibleSelectionEnd(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SSH_CONNECTION", "isolated-test")
	for _, down := range []bool{false, true} {
		a := stage7CopyApp(t)
		pid := a.cm.pane
		a.exitCopy()
		a.scroll[pid] = 24
		inner := a.innerRect(pid)
		top := viewTop(a.sup.Get(pid), 24, inner.H)
		a.sel = &selState{pane: pid, ax: 5, ay: top + inner.H/2, hx: 5, hy: top + inner.H/2, inner: inner}
		y := inner.Y - 2
		if down {
			y = inner.Y + inner.H + 1
		}
		event := uv.Mouse{X: inner.X + 5, Y: y, Button: uv.MouseLeft}
		a.selMouse(input.Event{Kind: input.KindMouseMotion, Mouse: event})
		want, _ := a.selText(a.sel)
		var out bytes.Buffer
		a.out = &out
		a.selMouse(input.Event{Kind: input.KindMouseRelease, Mouse: event})
		want = "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(strings.TrimRight(want, "\n"))) + "\a"
		if out.String() != want {
			t.Fatalf("outside release changed visible selection down=%v", down)
		}
	}
}
