package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kazuph/ikesu/internal/render"
)

func TestSidebarContinuousTwoServersEveryCell(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	h.modes(0, 0, 240, 60)
	type row struct {
		Machine       string
		Right, Header bool
		Row           render.SidebarRow
		Y             int
	}
	type record struct {
		Side  string
		Width int
		Rows  []row
	}
	var records []record
	rows := func() []row {
		var result []row
		for _, hit := range m.sidebar.Hits {
			if !hit.New {
				result = append(result, row{hit.Machine, hit.Right, hit.Header, hit.Row, hit.Rect.Y})
			}
		}
		return result
	}
	paint := func() {
		m.outputFence.Accept([]byte("\x1b[0n"))
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
	}
	drag := func(right bool, w int) {
		s := m.endpoints[m.active].snapshot.State
		start, target := s.SidebarArea().W-1, w-1
		if right {
			start = s.RightSidebarArea().X
			target = m.size.Cols - w
		}
		for _, raw := range []string{fmt.Sprintf("\x1b[<0;%d;6M", start+1), fmt.Sprintf("\x1b[<32;%d;6M", target+1), fmt.Sprintf("\x1b[<0;%d;6m", target+1)} {
			if err := m.input(mouseEvent(t, raw)); err != nil {
				t.Fatal(err)
			}
		}
		h.until(fmt.Sprintf("continuous drag right=%t width=%d start=%d target=%d beforeL=%d beforeR=%d pixel=%t", right, w, start, target, s.SidebarArea().W, s.RightSidebarArea().W, m.mouse.pixel), func() bool {
			s := m.endpoints[m.active].snapshot.State
			if right {
				return s.RightSidebarArea().W == w
			}
			return s.SidebarArea().W == w
		})
		paint()
	}
	for _, right := range []bool{false, true} {
		h.modes(0, 0, 240, 60)
		paint()
		expected := rows()
		side := "left"
		if right {
			side = "right"
		}
		_, upper, _ := m.endpoints[m.active].snapshot.State.SidebarBounds()
		for width := upper; width >= 2; width-- {
			drag(right, width)
			got := rows()
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("side=%s width=%d changes endpoint/row/order/Y\ngot=%+v\nwant=%+v", side, width, got, expected)
			}
			in := *m.endpoints[m.active].snapshot
			f := render.ComposeClient(in, &m.sidebar, "test", false).Frame
			for _, hit := range m.sidebar.Hits {
				if hit.Right != right || hit.New || hit.Header {
					continue
				}
				glyph := ""
				snap := m.endpoints[hit.Machine].snapshot
				switch hit.Row.Kind {
				case render.RowWorkspace:
					glyph = snap.Panes[snap.State.Workspaces[hit.Row.Ws].TabRef().Layout.Focused()].Status.Rune
				case render.RowAgent:
					if hit.Row.Line == 0 {
						glyph = snap.Panes[hit.Row.Pane].Status.Rune
					}
				case render.RowJob:
					glyph = "●"
				}
				x := min(1, width-2)
				if right {
					x = in.State.RightSidebarArea().X + min(2, width-1)
				}
				if glyph != "" && f.Cell(x, hit.Rect.Y).Content != glyph {
					t.Fatalf("side=%s width=%d machine=%s row=%+v Y=%d status=%q want=%q", side, width, hit.Machine, hit.Row, hit.Rect.Y, f.Cell(x, hit.Rect.Y).Content, glyph)
				}
			}
			records = append(records, record{side, width, got})
			if width == 2 || width == 3 || width == 4 || width == 5 || width == 10 || width == 17 || width == 26 {
				if root := os.Getenv("SIDEBAR_CONTINUOUS_EVIDENCE"); root != "" {
					if err := os.MkdirAll(root, 0700); err != nil {
						t.Fatal(err)
					}
					b, _ := json.Marshal(f)
					if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("servers-%s-width%d.json", side, width)), b, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	if root := os.Getenv("SIDEBAR_CONTINUOUS_EVIDENCE"); root != "" {
		b, _ := json.MarshalIndent(records, "", "  ")
		if err := os.WriteFile(filepath.Join(root, "servers-rows.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PASS %d widths from real endpoint snapshots: all machine/workspace/pane row identities, Y/order and left state cells", len(records))
}
