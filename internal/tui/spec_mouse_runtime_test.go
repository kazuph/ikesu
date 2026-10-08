package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"path/filepath"
	"testing"
)

func TestSPECDoubleClickReleaseMenuOrFourApplicationEvents(t *testing.T) {
	for _, tracking := range []bool{false, true} {
		t.Run(fmt.Sprint(tracking), func(t *testing.T) {
			a := specRuntimeApp(t)
			pid := a.focusedPane()
			inner := a.innerRect(pid)
			path := filepath.Join(t.TempDir(), "received")
			expected := "\x1b[<0;3;3M\x1b[<0;3;3m\x1b[<0;3;3M\x1b[<0;3;3m"
			command := "printf READY; exec sleep 60"
			if tracking {
				command = fmt.Sprintf("stty raw -echo; printf '\\033[?1000h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(expected), path)
			}
			p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			m := uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseLeft}
			for i := 0; i < 2; i++ {
				a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
				if a.menu != nil {
					t.Fatal("menu opened on press")
				}
				a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
				if i == 0 && a.menu != nil {
					t.Fatal("menu opened after first click")
				}
			}
			if tracking {
				if a.menu != nil || a.sel != nil {
					t.Fatal("application intercepted")
				}
				specWaitFile(t, path, []byte(expected))
			} else {
				if a.menu == nil {
					t.Fatal("second release missing menu")
				}
			}
		})
	}
}

func TestSPECWheelThreeRoutesActualPTYAndHostScroll(t *testing.T) {
	for _, mode := range []string{"reporting", "alternate", "host"} {
		t.Run(mode, func(t *testing.T) {
			a := specRuntimeApp(t)
			pid := a.focusedPane()
			inner := a.innerRect(pid)
			path := filepath.Join(t.TempDir(), "wheel")
			expected := ""
			command := "i=0; while [ $i -lt 100 ]; do printf 'line %s\\r\\n' $i; i=$((i+1)); done; printf READY; exec sleep 60"
			if mode == "reporting" {
				expected = "\x1b[<64;3;3M"
				command = fmt.Sprintf("stty raw -echo; printf '\\033[?1000h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(expected), path)
			}
			if mode == "alternate" {
				expected = "\x1b[A"
				command = fmt.Sprintf("stty raw -echo; printf '\\033[?1049hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(expected), path)
			}
			p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			a.onMouse(input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseWheelUp}})
			if mode == "host" {
				if a.scroll[pid] != 3 {
					t.Fatal("host notch", a.scroll[pid])
				}
			} else {
				specWaitFile(t, path, []byte(expected))
				if a.scroll[pid] != 0 {
					t.Fatal("application wheel scrolled host")
				}
			}
		})
	}
}
