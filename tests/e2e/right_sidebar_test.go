package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestRightSidebarAttachedClientRestoresSavedModes(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	first := start(t)
	first.waitFor("first shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	first.send("\x02b\x02b\x02B")
	first.send("printf 'SAVE_%s\\n' READY\r")
	first.waitFor("saved display state", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "SAVE_READY") })
	first.send("\x02q")
	second := startEnv(t, map[string]string{"IKESU_STATE_DIR": first.stateDir})
	second.waitFor("reattached shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "SAVE_READY") })
	second.send("set -- $(stty size); printf '\\nRESTORED_%s_%s\\n' \"$1\" \"$2\"\r")
	second.waitFor("restored geometry", 5*time.Second, func(tx string) bool { return strings.Contains(tx, fmt.Sprintf("RESTORED_%d_%d", termH-4, termW-2-2)) })
	if second.emu.CellAt(termW-2, 5).Content != "│" || second.emu.CellAt(0, 0).Content == "│" {
		t.Fatal("reattach lost right compact / left hidden modes")
	}
}

func TestRightSidebarNarrowTerminalAdaptsWithoutSaving(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	for _, local := range []bool{false, true} {
		mode := "server"
		if local {
			mode = "no-session"
		}
		t.Run(mode, func(t *testing.T) {
			s := startMode(t, nil, local)
			s.waitFor("shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			s.send("\x02B\x02B\x02B")
			s.send("printf 'PREF_%s\\n' READY\r")
			s.waitFor("explicit open preference", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "PREF_READY") })
			name := s.name
			if local {
				name = "default"
			}
			path := filepath.Join(s.stateDir, "client-shell", "local-"+name+".json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ cols, right, pty int }{{80, 2, 50}, {67, 0, 39}, {64, 0, 62}, {120, 26, 66}} {
				if err := pty.Setsize(s.pt, &pty.Winsize{Cols: uint16(tc.cols), Rows: termH}); err != nil {
					t.Fatal(err)
				}
				s.emu.Resize(tc.cols, termH)
				marker := fmt.Sprintf("NARROW_%d_%d", tc.cols, tc.pty)
				// SIGWINCH and input arrive independently. Wait for the actual
				// kernel resize before printing, without another input event.
				s.send(fmt.Sprintf("while :; do set -- $(stty size); [ \"$2\" = %d ] && break; sleep 0.01; done; printf '\\nNARROW_%d_%%s\\n' \"$2\"\r", tc.pty, tc.cols))
				s.waitFor("adapted kernel width "+marker, 5*time.Second, func(tx string) bool { return strings.Contains(tx, marker) })
				if tc.right > 0 && s.emu.CellAt(tc.cols-tc.right, 5).Content != "│" {
					t.Fatal("automatic right divider missing")
				}
				if tc.cols == 64 && !strings.Contains(s.text(), "switch") {
					t.Fatal("mobile switch control missing")
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Fatal("automatic adaptation saved preference", err)
				}
			}
		})
	}
}

func TestRightSidebarWidthFooterPresetsRealTerminal(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprint("no-session=", local), func(t *testing.T) {
			s := startMode(t, nil, local)
			s.waitFor("normal footer", 10*time.Second, func(text string) bool { return strings.Contains(text, " NORMAL ") })
			for _, want := range []string{" WIDE ", " NARROW ", " NORMAL "} {
				s.send(fmt.Sprintf("\x1b[<0;2;%dM\x1b[<0;2;%dm", termH, termH))
				s.waitFor(want, 5*time.Second, func(text string) bool { return strings.Contains(text, want) })
			}
		})
	}
}
