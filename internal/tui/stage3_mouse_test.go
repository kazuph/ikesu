package tui

import (
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage3RightClickStripsModifierThroughoutRealPTYGesture(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.sidebarConfig, _ = config.Decode([]byte("[ui]\nright_click_passthrough_modifier='ctrl'\n"))
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	path := filepath.Join(t.TempDir(), "mouse")
	want := "\x1b[<2;3;3M\x1b[<34;3;3M\x1b[<2;3;3m\x1b[<16;3;3M\x1b[<16;3;3m"
	command := fmt.Sprintf("stty raw -echo; printf '\\033[?1002h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(want), path)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseRight, Mod: uv.ModCtrl}
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
		a.onMouse(input.Event{Kind: kind, Mouse: m})
	}
	m.Button = uv.MouseLeft
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	specWaitFile(t, path, []byte(want))
}

func TestStage3ViewerNotificationSurvivesRightClickAndExpires(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	v := a.NewViewer(io.Discard, 120, 35, nil)
	v.ShowToast(a, "target", "body", "info", a.focusedPane())
	v.Draw(a, nil)
	r := v.app.toastRect
	if r.W == 0 || v.app.toast == nil {
		t.Fatal("notification absent from viewer")
	}
	v.Input(a, []byte(fmt.Sprintf("\x1b[<2;%d;%dM\x1b[<2;%d;%dm", r.X+2, r.Y+2, r.X+2, r.Y+2)))
	if v.app.toast == nil {
		t.Fatal("right click dismissed viewer notification")
	}
	v.app.toast.until = time.Now().Add(-time.Second)
	v.Tick(a)
	if v.app.toast != nil || a.toast != nil {
		t.Fatal("expired notification or viewer state leaked")
	}
}

func TestStage3TrackJumpThenDragUsesThumbTop(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, a.st.PaneBorders)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 200 ]; do echo LINE-$i; i=$((i+1)); done; printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + inner.W - 1, Y: inner.Y + 5, Button: uv.MouseLeft}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	if a.sb == nil || a.sb.grab != 0 || a.scroll[pid] == 0 {
		t.Fatal("track did not jump with a top-anchored drag", a.sb, a.scroll[pid])
	}
	m.Y = inner.Y + inner.H - 1
	a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
	if a.scroll[pid] != 0 {
		t.Fatal("track drag did not reach live bottom", a.scroll[pid])
	}
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	_, top := thumbGeom(inner.H, p.Snapshot().ScrollbackLen, 0)
	m.Y = inner.Y + top + 1
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	if a.sb == nil || a.sb.grab != 1 {
		t.Fatal("thumb press lost its grab offset", a.sb)
	}
}

func TestStage3ToastOnlyLeftClickNavigatesAndDismisses(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	target := a.focusedPane()
	a.st.Apply(model.CmdNewTab{Workspace: 0})
	before := a.focusedPane()
	for _, button := range []uv.MouseButton{uv.MouseRight, uv.MouseMiddle, uv.MouseLeft} {
		a.ShowToast("target", "body", "info", target)
		a.draw()
		m := uv.Mouse{X: a.toastRect.X + 1, Y: a.toastRect.Y + 1, Button: button}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
		if button == uv.MouseLeft {
			if a.toast != nil || a.focusedPane() != target {
				t.Fatal("left click did not navigate and dismiss", a.toast, a.focusedPane())
			}
		} else if a.toast == nil || a.focusedPane() != before {
			t.Fatal("non-left click changed notification or target", button, a.toast, a.focusedPane())
		}
		a.menu = nil
	}
}

func TestStage3ModifiedLeftPressDoesNotSelectHostText(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	for _, mod := range []uv.KeyMod{uv.ModShift, uv.ModCtrl, uv.ModAlt} {
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseLeft, Mod: mod}})
		if a.sel != nil {
			t.Fatalf("modifier %v started host selection", mod)
		}
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseLeft}})
	if a.sel == nil || a.sel.pane != pid || a.sel.ax != 2 {
		t.Fatal("plain press did not start selection", a.sel)
	}
}
