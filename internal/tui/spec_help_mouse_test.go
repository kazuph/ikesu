package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"testing"
	"time"
)

func TestSPECCompletedDoubleClickSameCellDeadline(t *testing.T) {
	now := time.Unix(100, 0)
	m := uv.Mouse{X: 8, Y: 4, Button: uv.MouseLeft}
	c := clickMark{pane: 3, x: 8, y: 4, at: now, completed: true}
	for _, tc := range []struct {
		pane, x, y      int
		delay           time.Duration
		completed, want bool
	}{
		{3, 8, 4, 500 * time.Millisecond, true, true},
		{3, 8, 4, 500*time.Millisecond + 1, true, false},
		{3, 8, 4, 0, false, false}, {4, 8, 4, 0, true, false},
		{3, 9, 4, 0, true, false}, {3, 8, 5, 0, true, false},
	} {
		c.completed = tc.completed
		m.X = tc.x
		m.Y = tc.y
		if got := c.matches(tc.pane, m, now.Add(tc.delay)); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestSPECHelpMouseCaptureWheelTrackAndThumbGrab(t *testing.T) {
	a := &App{st: model.New("", 80, 24), fwd: -1}
	a.openHelp()
	a.help.rect = layout.Rect{X: 5, Y: 1, W: 68, H: 22}
	h := a.help
	body := a.helpBody()
	limit := a.helpMaxScroll()
	send := func(kind input.Kind, x, y int, b uv.MouseButton) {
		a.onMouse(input.Event{Kind: kind, Mouse: uv.Mouse{X: x, Y: y, Button: b}})
	}
	send(input.KindMouseWheel, 8, 5, uv.MouseWheelDown)
	if h.scroll != 3 {
		t.Fatal("wheel not three rows", h.scroll)
	}
	send(input.KindMouseWheel, 8, 5, uv.MouseWheelUp)
	if h.scroll != 0 {
		t.Fatal("wheel up", h.scroll)
	}
	th, top := thumbGeom(body, limit, limit)
	grab := th - 1
	send(input.KindMouseClick, 71, 3+top+grab, uv.MouseLeft)
	if !h.dragging || h.grab != grab {
		t.Fatal("grab offset", h)
	}
	send(input.KindMouseMotion, 71, 3+body-th+grab, uv.MouseLeft)
	if h.scroll != limit {
		t.Fatal("thumb bottom", h.scroll, limit)
	}
	send(input.KindMouseRelease, 71, 22, uv.MouseLeft)
	if h.dragging {
		t.Fatal("release did not stop drag")
	}
	send(input.KindMouseClick, 71, 3, uv.MouseLeft)
	if !h.dragging || h.scroll != 0 {
		t.Fatal("track did not move directly", h)
	}
	send(input.KindMouseRelease, 71, 3, uv.MouseLeft)
	send(input.KindMouseClick, 8, 5, uv.MouseLeft)
	if a.help != h || a.sel != nil || a.sep != nil || a.sb != nil || a.menu != nil || a.fwd != -1 {
		t.Fatal("help leaked mouse")
	}
	send(input.KindMouseClick, 0, 0, uv.MouseLeft)
	if a.help != nil || a.sel != nil || a.menu != nil {
		t.Fatal("outside close leaked event")
	}
}

func TestSPECDangerousCancelOutsideAndOverlaysCapture(t *testing.T) {
	a := specRuntimeApp(t)
	a.openQuitDialog()

	before := a.st.ActiveWorkspace().TabRef().Layout.Fingerprint()
	outside := input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: 0, Y: 0, Button: uv.MouseLeft}}
	a.onMouse(outside)
	if a.dialog != nil || a.sel != nil || a.quit || a.st.ActiveWorkspace().TabRef().Layout.Fingerprint() != before {
		t.Fatal("dialog outside capture")
	}
	a.dialog = nil
	a.openMainMenu(10, 10)
	menu := a.menu
	a.onMouse(outside)
	if a.menu != menu || a.sel != nil || a.st.ActiveWorkspace().TabRef().Layout.Fingerprint() != before {
		t.Fatal("menu outside capture")
	}
	a.menu = nil
	a.openHelp()
	a.help.rect = layout.Rect{X: 10, Y: 10, W: 68, H: 20}
	a.onMouse(outside)
	if a.help != nil || a.sel != nil || a.st.ActiveWorkspace().TabRef().Layout.Fingerprint() != before {
		t.Fatal("help outside close capture")
	}
}
