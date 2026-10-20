package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/teststate"
	"golang.org/x/sys/unix"
)

func TestUnchangedResizePreservesTerminalStateRealPTY(t *testing.T) {
	teststate.Lock(t)
	for _, pixels := range []bool{false, true} {
		t.Run(fmt.Sprint("pixel-change=", pixels), func(t *testing.T) {
			s := NewSupervisor()
			defer s.KillAll()
			dir := t.TempDir()
			setup, probe := filepath.Join(dir, "setup"), filepath.Join(dir, "probe")
			command := fmt.Sprintf("while [ ! -f %s ]; do sleep .01; done; printf '\\033[2J\\033[H\\033[3;10r\\033[3g\\033[6G\\033H\\033[3;1HSCROLLTOP\\033[11;1HOUTSIDE\\033[16;1HREADY'; while [ ! -f %s ]; do sleep .01; done; printf '\\033[10;1H\\n\\033[14;1HA\\tB\\033[17;1HPROBED'; exec /bin/cat", setup, probe)
			p, err := s.SpawnWith(1, dir, 60, 20, nil, []string{"/bin/sh", "-c", command})
			if err != nil {
				t.Fatal(err)
			}
			s.ResizeWithPixels(1, 60, 20, 8, 16)
			if err := os.WriteFile(setup, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			waitReadMarker(t, p, "READY")
			before := p.Snapshot().Revision
			cw, ch := 8, 16
			if pixels {
				cw, ch = 10, 18
			}
			s.ResizeWithPixels(1, 60, 20, cw, ch)
			if !pixels && p.Snapshot().Revision != before {
				t.Fatal("identical resize changed terminal revision")
			}
			size, err := unix.IoctlGetWinsize(p.ptyFD, unix.TIOCGWINSZ)
			if err != nil {
				t.Fatal(err)
			}
			if size.Col != 60 || size.Row != 20 || int(size.Xpixel) != 60*cw || int(size.Ypixel) != 20*ch {
				t.Fatalf("kernel size: %+v", size)
			}
			if err := os.WriteFile(probe, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			waitReadMarker(t, p, "PROBED")
			lines, err := p.ReadLines("visible", true)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(lines, "\n"), "SCROLLTOP") || !strings.Contains(lines[10], "OUTSIDE") || lines[13] != "A    B" {
				t.Fatalf("scroll region or custom tab stop changed: %q", lines)
			}
		})
	}
}
