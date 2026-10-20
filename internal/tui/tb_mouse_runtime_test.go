package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestTBMotionAndHorizontalWheelActualPTY(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		name, modes  string
		cw, ch, x, y int
		covered      bool
	}{
		{"cell", "", 0, 0, 4, 5, false},
		{"pixel", "\x1b[?1016h", 8, 16, 29, 73, false},
		{"unknown-pixel", "\x1b[?1016h", 0, 0, 4, 5, false},
		{"menu-captures", "", 0, 0, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			a.cellW, a.cellH = tc.cw, tc.ch
			pid := a.focusedPane()
			inner := a.innerRect(pid)
			path := filepath.Join(t.TempDir(), "mouse")
			expected := fmt.Sprintf("\x1b[<35;%d;%dM\x1b[<66;%d;%dM\x1b[<67;%d;%dM|", tc.x, tc.y, tc.x, tc.y, tc.x, tc.y)
			if tc.covered {
				expected = "|"
			}
			command := fmt.Sprintf("stty raw -echo; printf '\\033[?1003h\\033[?1006h%sREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", tc.modes, len(expected), path)
			p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			if tc.covered {
				a.menu = &uiMenu{m: render.Menu{X: inner.X, Y: inner.Y, Items: []render.MenuItem{{Label: "Close pane"}}}}
			}
			m := uv.Mouse{X: inner.X + 3, Y: inner.Y + 4, Button: uv.MouseNone}
			a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
			for _, button := range []uv.MouseButton{uv.MouseWheelLeft, uv.MouseWheelRight} {
				m.Button = button
				a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: m})
			}
			// The barrier follows mouse reports through the same emulator response stream.
			p.WithEmu(func(e *vt.Emulator) { e.SendKey(uv.KeyPressEvent{Code: '|'}) })
			specWaitFile(t, path, []byte(expected))
			if !tc.covered && a.toast != nil {
				t.Fatalf("input created an unsupported toast: %+v", a.toast)
			}
		})
	}
}

func TestTBUnknownPixelGeometryRecoversAfterSizeReport(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	path := filepath.Join(t.TempDir(), "recovery")
	expected := "\x1b[<0;4;5M\x1b[<0;29;73M|"
	command := fmt.Sprintf("stty raw -echo; printf '\\033[?1003h\\033[?1006h\\033[?1016hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(expected), path)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + 3, Y: inner.Y + 4, Button: uv.MouseLeft}
	a.forwardTo(pid, uv.MouseClickEvent(m))
	specFeed(a, "\x1b[6;16;8t")
	a.forwardTo(pid, uv.MouseClickEvent(m))
	p.WithEmu(func(e *vt.Emulator) { e.SendKey(uv.KeyPressEvent{Code: '|'}) })
	specWaitFile(t, path, []byte(expected))
}
