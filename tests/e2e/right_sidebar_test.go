package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/creack/pty"
)

var rightSidebarEvidenceDir = os.Getenv("GTX_SIDEBAR_EVIDENCE")

// This drives the real binary and asks the shell for the kernel's PTY size.
func TestRightSidebarNineStatesRealTerminal(t *testing.T) {
	for _, local := range []bool{false, true} {
		mode := "server"
		if local {
			mode = "no-session"
		}
		t.Run(mode, func(t *testing.T) {
			s := startMode(t, nil, local)
			s.waitFor("shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
			widths := []int{26, 2, 0}
			for left := 0; left < 3; left++ {
				for right := 0; right < 3; right++ {
					cols, rows := termW-widths[left]-widths[right]-3, termH-4
					marker := fmt.Sprintf("SIZE_%d%d_%d_%d", left, right, rows, cols)
					s.send(fmt.Sprintf("set -- $(stty size); printf '\\nSIZE_%d%d_%%s_%%s\\n' \"$1\" \"$2\"\r", left, right))
					s.waitFor("kernel PTY size "+marker, 5*time.Second, func(tx string) bool { return strings.Contains(tx, marker) })
					if widths[left] > 0 && s.emu.CellAt(widths[left]-1, 5).Content != "│" {
						t.Fatal("left divider does not match PTY origin")
					}
					if widths[right] > 0 && s.emu.CellAt(termW-widths[right], 5).Content != "│" {
						t.Fatal("right divider does not match PTY edge")
					}
					saveRightSidebarScreen(t, s, fmt.Sprintf("%s-left%d-right%d", mode, left, right))
					s.send("\x02B")
				}
				s.send("\x02b")
			}
		})
	}
}

func saveRightSidebarScreen(t *testing.T, s *session, name string) {
	t.Helper()
	root := rightSidebarEvidenceDir
	if root == "" {
		return
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	grid := make([][]uv.Cell, termH)
	for y := range grid {
		grid[y] = make([]uv.Cell, termW)
		for x := range grid[y] {
			if c := s.emu.CellAt(x, y); c != nil {
				grid[y][x] = *c
			}
		}
	}
	data, err := json.Marshal(struct {
		Cols, Rows int
		Cells      [][]uv.Cell
	}{termW, termH, grid})
	if err != nil {
		t.Fatal(err)
	}
	for extension, data := range map[string][]byte{"json": data, "txt": []byte(s.text())} {
		if err := os.WriteFile(filepath.Join(root, name+"."+extension), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRightSidebarAttachedClientRestoresSavedModes(t *testing.T) {
	first := start(t)
	first.waitFor("first shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	first.send("\x02b\x02b\x02B")
	first.send("printf 'SAVE_%s\\n' READY\r")
	first.waitFor("saved display state", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "SAVE_READY") })
	first.send("\x02q")
	second := startEnv(t, map[string]string{"GTX_STATE_DIR": first.stateDir})
	second.waitFor("reattached shell", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "SAVE_READY") })
	second.send("set -- $(stty size); printf '\\nRESTORED_%s_%s\\n' \"$1\" \"$2\"\r")
	second.waitFor("restored geometry", 5*time.Second, func(tx string) bool { return strings.Contains(tx, fmt.Sprintf("RESTORED_%d_%d", termH-4, termW-2-3)) })
	if second.emu.CellAt(termW-2, 5).Content != "│" || second.emu.CellAt(0, 0).Content == "│" {
		t.Fatal("reattach lost right compact / left hidden modes")
	}
}

func TestRightSidebarNarrowTerminalAdaptsWithoutSaving(t *testing.T) {
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
			for _, tc := range []struct{ cols, right, pty int }{{80, 2, 49}, {67, 0, 38}, {64, 0, 61}, {120, 26, 65}} {
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
