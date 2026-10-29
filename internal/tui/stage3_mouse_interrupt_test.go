package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestStage3RightGestureInterruptionPreservesModifiersInRealPTY(t *testing.T) {
	for _, button := range []uv.MouseButton{uv.MouseWheelUp, uv.MouseLeft, uv.MouseMiddle} {
		t.Run(fmt.Sprint(button), func(t *testing.T) {
			teststate.Lock(t)
			a := specRuntimeApp(t)
			a.sidebarConfig, _ = config.Decode([]byte("[ui]\nright_click_passthrough_modifier='ctrl'\n"))
			pid := a.focusedPane()
			inner := a.innerRect(pid)
			path := filepath.Join(t.TempDir(), "mouse")
			code := map[uv.MouseButton]int{uv.MouseWheelUp: 80, uv.MouseLeft: 16, uv.MouseMiddle: 17}[button]
			want := fmt.Sprintf("\x1b[<2;3;3M\x1b[<%d;3;3M\x1b[<18;3;3m\x1b[<16;3;3M\x1b[<16;3;3m", code)
			command := fmt.Sprintf("stty raw -echo; printf '\\033[?1002h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(want), path)
			p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			m := uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseRight, Mod: uv.ModCtrl}
			a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
			m.Button = button
			kind := input.KindMouseClick
			if button == uv.MouseWheelUp {
				kind = input.KindMouseWheel
			}
			a.onMouse(input.Event{Kind: kind, Mouse: m})
			m.Button = uv.MouseRight
			a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
			m.Button = uv.MouseLeft
			a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
			a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
			specWaitFile(t, path, []byte(want))
		})
	}
}

func TestStage3LocalTrackOnlyJumpsAndThumbRetainsGrab(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.sidebarConfig.UI.PaneScrollbars = "always"
	pid := a.focusedPane()
	inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, a.st.PaneBorders)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 200 ]; do echo LINE-$i; i=$((i+1)); done; printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + inner.W - 1, Y: inner.Y + 5, Button: uv.MouseLeft}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	jump := a.scroll[pid]
	if a.sb != nil || jump == 0 {
		t.Fatal("local track did not jump without dragging", a.sb, jump)
	}
	m.Y = inner.Y + inner.H - 1
	a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	if a.scroll[pid] != jump {
		t.Fatal("local track motion changed position", a.scroll[pid], jump)
	}
	_, top := thumbGeom(inner.H, p.Snapshot().ScrollbackLen, jump)
	m.Y = inner.Y + top + 1
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	if a.sb == nil || a.sb.grab != 1 {
		t.Fatal("local thumb lost grab offset", a.sb)
	}
	m.Y = inner.Y + inner.H - 1
	a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
	if a.scroll[pid] != 0 {
		t.Fatal("local thumb did not drag to bottom", a.scroll[pid])
	}
}
