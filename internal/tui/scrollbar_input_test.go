package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"path/filepath"
	"testing"
	"time"
)

func TestScrollbarLastColumnActualPTY(t *testing.T) {
	teststate.Lock(t)
	for _, history := range []bool{false, true} {
		for _, mode := range []string{"auto-active", "auto-expired", "always", "off"} {
			t.Run(fmt.Sprintf("%s/history=%t", mode, history), func(t *testing.T) {
				a := specRuntimeApp(t)
				pid := a.focusedPane()
				inner := a.innerRect(pid)
				visible := history && (mode == "always" || mode == "auto-active")
				expected := fmt.Sprintf("\x1b[<35;%d;2M\x1b[<0;%d;2M\x1b[<32;%d;2M\x1b[<0;%d;2m\x1b[<64;%d;2M|", inner.W, inner.W, inner.W, inner.W, inner.W)
				if visible {
					expected = "|"
				}
				path := filepath.Join(t.TempDir(), "mouse")
				output := ""
				if history {
					output = "i=0; while [ $i -lt 100 ]; do printf 'row\\r\\n'; i=$((i+1)); done; "
				}
				command := fmt.Sprintf("stty raw -echo; %sprintf '\\033[?1003h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", output, len(expected), path)
				p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
				if err != nil {
					t.Fatal(err)
				}
				specWaitText(t, p, "READY")
				n := p.Snapshot().ScrollbackLen
				if (n > 0) != history {
					t.Fatal("incorrect history fixture", n)
				}
				if mode == "always" || mode == "off" {
					a.sidebarConfig.UI.PaneScrollbars = config.ScrollbarMode(mode)
				} else {
					a.hostScroll(pid, n, true)
					// Keep the tracking application on its live screen while retaining the interaction deadline.
					a.scroll[pid] = 0
					if mode == "auto-expired" {
						a.expirePaneScrollbars(a.scrollbarUntil[pid])
					}
				}
				m := uv.Mouse{X: inner.X + inner.W - 1, Y: inner.Y + 1, Button: uv.MouseNone}
				a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
				m.Button = uv.MouseLeft
				for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
					a.onMouse(input.Event{Kind: kind, Mouse: m})
				}
				m.Button = uv.MouseWheelUp
				a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: m})
				p.WithEmu(func(e *vt.Emulator) { e.SendKey(uv.KeyPressEvent{Code: '|'}) })
				specWaitFile(t, path, []byte(expected))
			})
		}
	}
}

func TestScrollbarZeroHistoryWheelThenLastColumnSelection(t *testing.T) {
	teststate.Lock(t)
	for _, mode := range []string{"auto", "always", "off"} {
		t.Run(mode, func(t *testing.T) {
			a := specRuntimeApp(t)
			a.sidebarConfig.UI.PaneScrollbars = config.ScrollbarMode(mode)
			pid := a.focusedPane()
			r := a.innerRect(pid)
			p, err := a.sup.SpawnWith(pid, a.cwd, r.W, r.H, nil, []string{"/bin/sh", "-c", "printf READY; exec sleep 60"})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			if p.Snapshot().ScrollbackLen != 0 {
				t.Fatal("fixture has history")
			}
			m := uv.Mouse{X: r.X + r.W - 1, Y: r.Y + 1, Button: uv.MouseWheelUp}
			a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: m})
			m.Button = uv.MouseLeft
			a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
			if a.sel == nil || a.sb != nil {
				t.Fatal("invisible scrollbar stole selection", a.sel, a.sb)
			}
			m.X--
			a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: m})
			if a.sel == nil {
				t.Fatal("selection drag lost")
			}
		})
	}
}

func TestScrollbarVisibilityGeometryAndModes(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	a.showPaneScrollbar(pid)
	now := time.Now()
	for _, w := range []int{4, 5} {
		for _, h := range []int{0, 1} {
			for _, n := range []int{0, 1} {
				for _, mode := range []string{"auto", "always", "off"} {
					a.sidebarConfig.UI.PaneScrollbars = config.ScrollbarMode(mode)
					want := w >= 5 && h > 0 && n > 0 && mode != "off"
					if got := a.paneScrollbarVisible(pid, model.Rect{W: w, H: h}, n, now); got != want {
						t.Fatal(w, h, n, mode, got, want)
					}
				}
			}
		}
	}
}
