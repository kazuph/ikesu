package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
)

type visibleImageCell struct{ X, Y, Row, Col int }

func TestKittyVirtualActualPTYCellsAndHandoff(t *testing.T) {
	for _, format := range []int{24, 100} {
		t.Run(fmt.Sprintf("format%d", format), func(t *testing.T) { testKittyVirtual(t, format) })
	}
}

func testKittyVirtual(t *testing.T, format int) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[experimental]\nkitty_graphics=true\n[ui]\nmouse_scroll_lines=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	var raw bytes.Buffer
	s.observeOutput = func(b []byte) { raw.Write(b) }
	defer func() {
		if dir := os.Getenv("HANDOFF_EVIDENCE_DIR"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("virtual-client-%d.bin", format)), raw.Bytes(), 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	var outer kitty.Store
	s.emu.RegisterApcHandler(func(data []byte) bool {
		pos := s.emu.CursorPosition()
		_, _, _, handled := outer.Handle(data, pos.X, pos.Y, 0, 0, false)
		return handled
	})
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	borderX, borderY := -1, -1
	for y := 0; y < termH && borderX < 0; y++ {
		for x := 1; x < termW; x++ {
			if s.emu.CellAt(x, y).Content == "┌" {
				borderX, borderY = x, y
				break
			}
		}
	}
	if borderX < 0 {
		t.Fatal("no pane border")
	}
	probe, pids := filepath.Join(root, "virtual.py"), filepath.Join(root, "pids")
	program := `import os,sys,tty,base64
tty.setraw(0)
open(sys.argv[1],'w').write(str(os.getpid())+'\n')
os.write(1,b'\x1b[2J\x1b[3J\x1b[H')
pixels=open(sys.argv[2],'rb').read()
os.write(1,b'\x1b_Ga=T,U=1,q=2,f='+sys.argv[3].encode()+b',s=4,v=2,i=33554474,p=7,c=4,r=2;'+base64.b64encode(pixels)+b'\x1b\\')
marks=['\u0305','\u030d','\u030e','\u0310']
for row in range(2):
 text='\x1b['+str(row+1)+';7H\x1b[38;5;42m\x1b[58;5;7m'
 for col in range(4):
  text+='\U0010eeee'
  if row==0 and col==2: text+=marks[row]
  elif row==0 and col==3: pass
  else: text+=marks[row]+marks[col]+marks[2]
 text+='\x1b[0m'
 os.write(1,text.encode())
os.write(1,b'\x1b]2;VIRTUAL_READY\x07')
while True:
 command=os.read(0,1)
 if command==b'x':
  open(sys.argv[1],'a').write(str(os.getpid())+'\n')
  os.write(1,b'\x1b]2;VIRTUAL_CONTINUES\x07')
 elif command==b's': os.write(1,b'\x1b[999;1H\nSCROLL_READY')
 else: break
`
	if err := os.WriteFile(probe, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	expectedPixels := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255, 0, 255, 255, 255, 0, 255, 255, 255, 0, 0, 0, 0}
	if format == 100 {
		picture := image.NewNRGBA(image.Rect(0, 0, 4, 2))
		for i := 0; i < 8; i++ {
			picture.SetNRGBA(i%4, i/4, color.NRGBA{R: expectedPixels[i*3], G: expectedPixels[i*3+1], B: expectedPixels[i*3+2], A: 255})
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		expectedPixels = encoded.Bytes()
	}
	source := filepath.Join(root, "pixels")
	if err := os.WriteFile(source, expectedPixels, 0600); err != nil {
		t.Fatal(err)
	}
	cells := func() []visibleImageCell { return decodeVirtualCells(t, s, &outer, expectedPixels) }
	s.send(fmt.Sprintf("/usr/bin/python3 %q %q %q %d\r", probe, pids, source, format))
	s.waitFor("eight actual Unicode image cells", 10*time.Second, func(string) bool { return len(cells()) == 8 })
	initial := cells()
	for i, c := range initial {
		row, col := i/4, i%4
		if c != (visibleImageCell{X: borderX + 1 + 6 + col, Y: borderY + 1 + row, Row: row, Col: col}) {
			t.Fatalf("wrong initial image part/position: %+v", initial)
		}
	}
	before, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	s.send("x")
	s.waitFor("virtual image survives handoff", 10*time.Second, func(tx string) bool {
		return strings.Contains(tx, "VIRTUAL_CONTINUES") && reflect.DeepEqual(cells(), initial)
	})
	after, err := os.ReadFile(pids)
	if err != nil || string(after) != string(before)+string(before) {
		t.Fatalf("virtual program PID changed: %q %q %v", before, after, err)
	}
	s.send("s")
	s.waitFor("only lower source row remains at pane top", 5*time.Second, func(string) bool { return len(cells()) == 4 })
	partial := cells()
	for col, c := range partial {
		if c != (visibleImageCell{X: initial[col].X, Y: initial[col].Y, Row: 1, Col: col}) {
			t.Fatalf("partial clipping displayed wrong source range: %+v", partial)
		}
	}
	s.send(fmt.Sprintf("\x1b[<64;%d;%dM", initial[0].X+1, initial[0].Y+1))
	s.waitFor("scrollback restores both image rows", 5*time.Second, func(string) bool { return reflect.DeepEqual(cells(), initial) })
	s.send(fmt.Sprintf("\x1b[<65;%d;%dM", initial[0].X+1, initial[0].Y+1))
	s.waitFor("scrollback returns to clipped lower row", 5*time.Second, func(string) bool { return reflect.DeepEqual(cells(), partial) })
	s.rpc(api.MethodTabCreate, api.TabCreateParams{Focus: true}, nil)
	s.waitFor("tab hides virtual image", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 && len(cells()) == 0 })
	s.rpc(api.MethodTabFocus, api.TabTarget{TabID: "s1:t1"}, nil)
	s.waitFor("tab restores virtual image", 5*time.Second, func(string) bool { return reflect.DeepEqual(cells(), partial) })
	s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, nil)
	s.waitFor("workspace hides virtual image", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 && len(cells()) == 0 })
	s.rpc(api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: "s1"}, nil)
	s.waitFor("workspace restores virtual image", 5*time.Second, func(string) bool { return reflect.DeepEqual(cells(), partial) })
	paneID := "p1"
	var split api.PaneInfoResult
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &paneID, Direction: "right", Focus: false}, &split)
	s.waitFor("split keeps visible source row", 5*time.Second, func(string) bool { return len(cells()) == 4 })
	s.rpc(api.MethodPaneSwap, api.PaneSwapParams{PaneID: &paneID, TargetPaneID: &split.Pane.PaneID}, nil)
	s.waitFor("move shifts source row", 5*time.Second, func(string) bool { return len(cells()) == 4 && cells()[0].X > partial[0].X })
	moved := cells()
	amount := 0.05
	s.rpc(api.MethodPaneResize, api.PaneResizeParams{PaneID: &paneID, Direction: "left", Amount: &amount}, nil)
	s.waitFor("resize shifts source row", 5*time.Second, func(string) bool { return len(cells()) == 4 && cells()[0].X != moved[0].X })
	resized := cells()
	for col, c := range resized {
		if c.Row != 1 || c.Col != col || c.Y != partial[col].Y {
			t.Fatalf("move/resize changed source part: %+v", resized)
		}
	}
	s.rpc(api.MethodPaneClose, api.PaneTarget{PaneID: "p1"}, nil)
	s.waitFor("close removes virtual prototype and cells", 5*time.Second, func(string) bool { return len(outer.Placements) == 0 && len(cells()) == 0 })
	t.Logf("actual U=1,q=2,f=%d program PID=%s; initial cells=%+v; clipped source row=%+v; moved=%+v; resized=%+v; tab/workspace/close hide image; original ID=33554474 and source pixels preserved", format, strings.TrimSpace(string(before)), initial, partial, moved, resized)
}

// Independently decode the explicit cells emitted by the client. The source
// program also uses omitted marks; the outer client must resolve all of them.
func decodeVirtualCells(t *testing.T, s *session, outer *kitty.Store, pixels []byte) []visibleImageCell {
	t.Helper()
	var out []visibleImageCell
	marks := map[rune]int{'\u0305': 0, '\u030d': 1, '\u030e': 2, '\u0310': 3}
	for y := 0; y < termH; y++ {
		for x := 0; x < termW; x++ {
			cell := s.emu.CellAt(x, y)
			chars := []rune(cell.Content)
			if len(chars) == 0 || chars[0] != '\U0010eeee' {
				continue
			}
			if len(chars) != 4 || chars[3] != '\u0305' || cell.Style.Fg == nil {
				t.Fatalf("outer placeholder was not normalized: (%d,%d) %U", x, y, chars)
			}
			r, g, b, _ := cell.Style.Fg.RGBA()
			id := uint32(r>>8)<<16 | uint32(g>>8)<<8 | uint32(b>>8)
			var prototype *kitty.Placement
			for i := range outer.Placements {
				p := &outer.Placements[i]
				if p.Virtual && p.Image.ID == id && p.ID == 7 {
					prototype = p
				}
			}
			if prototype == nil {
				// Text and APC can arrive in separate PTY reads. The next
				// completed frame must contain both before it passes.
				continue
			}
			if !bytes.Equal(prototype.Image.Data, pixels) || prototype.Cols != 4 || prototype.Rows != 2 || id == 33554474 {
				t.Fatalf("wrong source/prototype: %+v", prototype)
			}
			row, rowOK := marks[chars[1]]
			col, colOK := marks[chars[2]]
			if !rowOK || !colOK || row >= 2 || col >= 4 {
				t.Fatalf("invalid source range: %U", chars)
			}
			out = append(out, visibleImageCell{X: x, Y: y, Row: row, Col: col})
		}
	}
	return out
}
