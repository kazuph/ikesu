package e2e

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
)

func TestKittyActualPTYPlacementAndHandoff(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n[ui]\nmouse_scroll_lines=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	var outer kitty.Store
	var placed []kitty.Placement
	s.emu.RegisterApcHandler(func(data []byte) bool {
		pos := s.emu.CursorPosition()
		_, _, _, handled := outer.Handle(data, pos.X, pos.Y, 0, 0, false)
		if strings.Contains(string(data), "a=p,") {
			placed = append(placed, outer.Placements[len(outer.Placements)-1])
		}
		return handled
	})
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	// Derive the pane origin from the actual rendered border, independently
	// of the graphics encoder and its pane rectangle calculation.
	borderX, borderY := -1, -1
	for y := 0; y < termH; y++ {
		for x := 1; x < termW; x++ {
			if s.emu.CellAt(x, y).Content == "┌" {
				borderX, borderY = x, y
				break
			}
		}
		if borderX >= 0 {
			break
		}
	}
	if borderX < 0 {
		t.Fatalf("pane border not found:\n%s", s.text())
	}
	probe := filepath.Join(root, "probe.py")
	answers := filepath.Join(root, "answers")
	script := `import os,sys,tty,select,time
tty.setraw(0)
os.write(1,b'\x1b[>q\x1b_Ga=q,t=d,f=24,s=1,v=1,i=31;AP8A\x1b\\')
data=b''
end=time.monotonic()+5
while time.monotonic()<end and data.count(b'\x1b\\')<2:
 if select.select([0],[],[],end-time.monotonic())[0]: data+=os.read(0,4096)
open(sys.argv[1],'wb').write(data)
os.write(1,b'\x1b[5;7H\x1b_Ga=T,t=d,f=24,s=1,v=1,i=32,c=3,r=2,C=1;AP8A\x1b\\\x1b]2;GRAPHICS_READY\x07')
os.read(0,4096)
while True:
 command=os.read(0,1)
 if command==b'x': os.write(1,b'KITTY_CONTINUES')
 elif command==b's': os.write(1,b'\x1b[999;1H\nSCROLL_READY')
 else: break
`
	if err := os.WriteFile(probe, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	s.send(fmt.Sprintf("/usr/bin/python3 %q %q\r", probe, answers))
	s.waitFor("actual outer placement", 10*time.Second, func(tx string) bool { return len(placed) > 0 })
	p := placed[len(placed)-1]
	if p.X != borderX+1+6 || p.Y != borderY+1+4 || p.Cols != 3 || p.Rows != 2 || !bytes.Equal(p.Image.Data, []byte{0, 255, 0}) {
		t.Fatalf("actual outer placement=%+v; pane border=%d,%d", p, borderX, borderY)
	}
	b, err := os.ReadFile(answers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("\x1bP>|ghostty (ikesu ")) || !bytes.Contains(b, []byte("\x1b_Gi=31;OK\x1b\\")) {
		t.Fatalf("version/query responses=%q", b)
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	s.send("x")
	s.waitFor("same image after handoff", 10*time.Second, func(tx string) bool { return strings.Contains(tx, "KITTY_CONTINUES") })
	if len(outer.Placements) != 1 || outer.Placements[0].X != p.X || outer.Placements[0].Y != p.Y || !bytes.Equal(outer.Placements[0].Image.Data, p.Image.Data) {
		t.Fatalf("image lost after handoff: %+v", outer.Placements)
	}
	var tab api.TabInfoResult
	s.rpc(api.MethodTabCreate, api.TabCreateParams{Focus: true}, &tab)
	s.waitFor("tab switch removes image", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 })
	s.rpc(api.MethodTabFocus, api.TabTarget{TabID: "s1:t1"}, nil)
	s.waitFor("tab return restores image", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 })
	var workspace api.WorkspaceInfoResult
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, &workspace)
	s.waitFor("workspace switch removes image", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 })
	s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: "s1"}, nil)
	s.waitFor("workspace return restores image", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 })
	paneID := "p1"
	var split api.PaneInfoResult
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &paneID, Direction: "right", Focus: false}, &split)
	s.waitFor("split retains image", 5*time.Second, func(tx string) bool { return countPanes(tx) >= 2 && len(outer.Placements) == 1 })
	s.rpc(api.MethodPaneSwap, api.PaneSwapParams{PaneID: &paneID, TargetPaneID: &split.Pane.PaneID}, nil)
	s.waitFor("pane move shifts image", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 && outer.Placements[0].X > p.X })
	moved := outer.Placements[0]
	if moved.Y != p.Y || moved.Cols != p.Cols || moved.Rows != p.Rows || !bytes.Equal(moved.Image.Data, p.Image.Data) {
		t.Fatalf("moved placement changed source or cell size: %+v", moved)
	}
	amount := 0.05
	s.rpc(api.MethodPaneResize, api.PaneResizeParams{PaneID: &paneID, Direction: "left", Amount: &amount}, nil)
	s.waitFor("pane resize shifts image", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 && outer.Placements[0].X != moved.X })
	resized := outer.Placements[0]
	if resized.Y != moved.Y || resized.Cols != 3 || resized.Rows != 2 {
		t.Fatalf("resize changed image cell geometry: %+v", resized)
	}
	s.send("s")
	s.waitFor("PTY scroll moves image one row", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 && outer.Placements[0].Y == resized.Y-1 })
	s.send(fmt.Sprintf("\x1b[<64;%d;10M", resized.X+1))
	s.waitFor("scrollback restores image row", 5*time.Second, func(string) bool { return len(outer.Placements) == 1 && outer.Placements[0].Y == resized.Y })
	beforeResize := len(placed)
	if err := pty.Setsize(s.pt, &pty.Winsize{Cols: termW + 10, Rows: termH + 2}); err != nil {
		t.Fatal(err)
	}
	s.emu.Resize(termW+10, termH+2)
	s.waitFor("host resize repaints image", 5*time.Second, func(string) bool { return len(placed) > beforeResize && len(outer.Placements) == 1 })
	if outer.Placements[0].Cols != 3 || outer.Placements[0].Rows != 2 || !bytes.Equal(outer.Placements[0].Image.Data, p.Image.Data) {
		t.Fatalf("host resize lost pixels: %+v", outer.Placements)
	}
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: "p1"}, nil)
	s.waitFor("pane close deletes image", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 })
	t.Logf("tab/workspace switch removed and restored image; split and swap moved x=%d to x=%d; pane close removed image", p.X, moved.X)
	t.Logf("resize moved x=%d to x=%d; PTY scroll moved y=%d to y=%d; scrollback returned to y=%d", moved.X, resized.X, resized.Y, resized.Y-1, resized.Y)
	t.Logf("actual outer kitty placement x=%d y=%d c=%d r=%d; pane border x=%d y=%d; source pixels=%s; responses=%s", p.X, p.Y, p.Cols, p.Rows, borderX, borderY, hex.EncodeToString(p.Image.Data), hex.EncodeToString(b))
}
