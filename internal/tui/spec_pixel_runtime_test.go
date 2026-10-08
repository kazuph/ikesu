package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"path/filepath"
	"strings"
	"testing"
)

func TestSPECCellCenterPixelButtonMotionWheelActualBytes(t *testing.T) {
	a := specRuntimeApp(t)
	a.cellW = 16
	a.cellH = 32
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	path := filepath.Join(t.TempDir(), "pixels")
	expected := "\x1b[<0;57;145M\x1b[<32;57;145M\x1b[<64;57;145M\x1b[<0;57;145m"
	command := fmt.Sprintf("stty raw -echo; printf '\\033[?1003h\\033[?1006h\\033[?1016hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(expected), path)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + 3, Y: inner.Y + 4, Button: uv.MouseLeft}
	a.forwardTo(pid, uv.MouseClickEvent(m))
	a.forwardTo(pid, uv.MouseMotionEvent(m))
	m.Button = uv.MouseWheelUp
	a.forwardTo(pid, uv.MouseWheelEvent(m))
	m.Button = uv.MouseLeft
	a.forwardTo(pid, uv.MouseReleaseEvent(m))
	specWaitFile(t, path, []byte(expected))
	a.cellW = 0
	a.cellH = 0
	a.forwardTo(pid, uv.MouseClickEvent(m))
	if a.toast == nil || !strings.Contains(a.toast.body, "requires known pixel geometry") {
		t.Fatal("unknown geometry substituted instead of unsupported error")
	}
}
