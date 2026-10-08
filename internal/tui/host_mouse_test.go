package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostMouseNegotiatesOnlyWithSupportAndGeometry(t *testing.T) {
	a := specRuntimeApp(t)
	out, err := os.CreateTemp(t.TempDir(), "host-output")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	a.out = out
	a.BeginHostMouse(0, 0)
	specFeed(a, "\x1b[?1016;2$y")
	if a.hostPixelMouse {
		t.Fatal("enabled with unknown geometry")
	}
	specFeed(a, "\x1b[6;32;16t")
	if !a.hostPixelMouse || a.cellW != 16 || a.cellH != 32 {
		t.Fatal("cell-size query did not negotiate", a.cellW, a.cellH)
	}
	b, _ := os.ReadFile(out.Name())
	if !strings.Contains(string(b), "\x1b[?1016h") {
		t.Fatal("pixel mode never requested")
	}
	a.BeginHostMouse(1920, 1120)
	if a.hostPixelMouse || a.cellW != 16 || a.cellH != 32 {
		t.Fatal("fresh attach did not reset")
	}
	specFeed(a, "\x1b[?1016;0$y")
	if a.hostPixelMouse {
		t.Fatal("unsupported terminal enabled pixel mouse")
	}
	a.BeginHostMouse(0, 0)
	specFeed(a, "\x1b[4;1120;1920t\x1b[?1016;2$y")
	if !a.hostPixelMouse || a.cellW != 16 || a.cellH != 32 {
		t.Fatal("pixel-size query did not negotiate")
	}
}

func TestSPECHostPixelPrecisionAndCellPaneActualBytes(t *testing.T) {
	for _, pixel := range []bool{false, true} {
		t.Run(fmt.Sprint(pixel), func(t *testing.T) {
			a := specRuntimeApp(t)
			a.BeginHostMouse(1920, 1120)
			specFeed(a, "\x1b[?1016;2$y")
			pid := a.focusedPane()
			inner := a.innerRect(pid)
			path := filepath.Join(t.TempDir(), "received")
			expected := "\x1b[<0;53;133M\x1b[<32;53;133M\x1b[<64;53;133M\x1b[<0;53;133m"
			modes := "\\033[?1003h\\033[?1006h\\033[?1016h"
			if !pixel {
				modes = "\\033[?1003h\\033[?1006h"
				expected = "\x1b[<0;4;5M\x1b[<32;4;5M\x1b[<64;4;5M\x1b[<0;4;5m"
			}
			command := fmt.Sprintf("stty raw -echo; printf '%sREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", modes, len(expected), path)
			p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			x := inner.X*16 + 53
			y := inner.Y*32 + 133
			for _, ev := range []struct {
				button int
				final  string
			}{{0, "M"}, {32, "M"}, {64, "M"}, {0, "m"}} {
				specFeed(a, fmt.Sprintf("\x1b[<%d;%d;%d%s", ev.button, x, y, ev.final))
			}
			specWaitFile(t, path, []byte(expected))
			if a.mousePixel != nil || a.scroll[pid] != 0 {
				t.Fatal("pixel metadata leaked or host scroll changed")
			}
		})
	}
}
