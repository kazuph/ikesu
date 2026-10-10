package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestCopyWheelEndpointKeepsViewportAndStandaloneScrollsRealPTY(t *testing.T) {
	teststate.Lock(t)
	for _, endpoint := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "endpoint"}[endpoint], func(t *testing.T) {
			a := specRuntimeApp(t)
			if endpoint {
				a.SetServerUI()
			}
			inner := a.innerRect(a.focusedPane())
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, inner.W, inner.H, nil,
				[]string{"/bin/sh", "-c", "i=0; while [ $i -lt 100 ]; do printf 'LINE-%s\\n' $i; i=$((i+1)); done; printf 'WHEEL_READY\\n'; exec /bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "WHEEL_READY")
			a.enterCopy(false)
			specFeed(a, "kkkkkkkkkk")
			before := *a.cm
			a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseWheelUp}})
			want := before.top - 3
			if endpoint {
				want = before.top
			}
			if a.cm.top != want || a.cm.cy != before.cy+want-before.top {
				t.Fatal("copy wheel viewport or cursor differs", before, a.cm)
			}
		})
	}
}

func TestCopySelectionContinuesAfterShrinkingTo100x25RealPTY(t *testing.T) {
	teststate.Lock(t)
	for _, endpoint := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "endpoint"}[endpoint], func(t *testing.T) {
			a := specRuntimeApp(t)
			if endpoint {
				a.SetServerUI()
			}
			a.exec(model.CmdResize{Cols: 160, Rows: 45})
			inner := a.innerRect(a.focusedPane())
			p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, inner.W, inner.H, nil,
				[]string{"/bin/sh", "-c", "i=0; while [ $i -lt 70 ]; do printf '%0120d\\n' $i; i=$((i+1)); done; printf 'RESIZE_READY\\n'; exec /bin/cat"})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "RESIZE_READY")
			a.enterCopy(false)
			specFeed(a, "g$vjj")
			if a.cm.anchor == nil || a.cm.cx < 100 {
				t.Fatal("fixture did not select beyond the new width")
			}
			a.exec(model.CmdResize{Cols: 100, Rows: 25})
			w, h := a.copyInnerW(), a.copyInnerH()
			if a.cm == nil || a.cm.anchor == nil || a.cm.cx < 0 || a.cm.cx >= w || a.cm.cy < a.cm.top || a.cm.cy >= a.cm.top+h {
				t.Fatal("copy cursor escaped the resized viewport", a.cm)
			}
			if a.cm.anchor.x < 0 || a.cm.anchor.x >= w {
				t.Fatal("selection anchor escaped the resized width", a.cm.anchor)
			}
			a.draw()
			before := a.cm.cx
			specFeed(a, "h")
			if a.cm.cx != before-1 || a.cm.anchor == nil {
				t.Fatal("selection could not continue after resizing")
			}
			specFeed(a, "\x1bq")
			if a.cm != nil {
				t.Fatal("copy mode did not exit after clearing selection")
			}
			a.exec(model.CmdResize{Cols: 160, Rows: 45})
			a.enterCopy(false)
			specFeed(a, "gvjjjjjjjjjjjjjjjjjjjjjjjjjjjjjj")
			a.exec(model.CmdResize{Cols: 100, Rows: 25})
			if a.cm.anchor == nil || a.cm.anchor.y < a.cm.top || a.cm.anchor.y >= a.cm.top+a.copyInnerH() {
				t.Fatal("selection anchor escaped the resized height", a.cm)
			}
			a.draw()
			specFeed(a, "k\x1bq")
			if _, err := p.WriteToPTY([]byte("AFTER_COPY_RESIZE\r")); err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "AFTER_COPY_RESIZE")
		})
	}
}
