package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestG2CopyStatusNarrowBoundsAndPresetPrecedence(t *testing.T) {
	for _, w := range []int{0, 1, 6, 8, 18, 26} {
		for _, h := range []int{0, 1, 3} {
			s := model.New("/tmp", w, h)
			f := NewFrame(w, h)
			drawSidebar(f, s, Input{State: s, Copied: " Copied 3 lines "})
			if h == 0 || w == 0 {
				continue
			}
			if f.Cell(w-1, h-1).Content != "│" {
				t.Fatal("right edge overwritten", w, h)
			}
			if w > 1 {
				line := frameLine(f, h-1)
				if strings.Contains(line, "NORMAL") {
					t.Fatal("copy mixed with preset", w, h, line)
				}
				if !reflect.DeepEqual(f.Cell(0, h-1).Style.Fg, Green) {
					t.Fatal("copy color", w, h)
				}
				if w < 16 && !strings.Contains(line, "…") {
					t.Fatal("copy not ellipsized", w, h, line)
				}
			}
		}
	}
}

func TestG2DraggedWorkspaceBothRowsHighlight(t *testing.T) {
	s := model.New("/tmp", 100, 40)
	s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/other"})
	s.WorkspaceDensity = "slim"
	out := Compose(Input{State: s, Dragging: true, DraggedWorkspace: 0})
	rows := SidebarRows(s, nil)
	_, y, w, _ := SidebarContent(s.SidebarArea())
	for i, r := range rows {
		if r.Ws == 0 && (r.Kind == RowWorkspace || r.Kind == RowGit) {
			if out.Frame.Cell(0, y+i).Content != "▌" {
				t.Fatal("dragged accent")
			}
			for x := 1; x < w; x++ {
				c := out.Frame.Cell(x, y+i)
				if c.Width > 0 && !reflect.DeepEqual(c.Style.Bg, Surface) {
					t.Fatal("dragged background", x)
				}
			}
		}
	}
}

func TestG2SelectedWorkspaceBothRowsHighlight(t *testing.T) {
	s := model.New("/tmp", 100, 40)
	s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/other"})
	s.WorkspaceDensity = "slim"
	selected := 0
	out := Compose(Input{State: s, SelectedWorkspace: &selected})
	rows := SidebarRows(s, nil)
	_, y, w, _ := SidebarContent(s.SidebarArea())
	for i, r := range rows {
		if r.Ws == selected && (r.Kind == RowWorkspace || r.Kind == RowGit) {
			if out.Frame.Cell(0, y+i).Content != "▌" {
				t.Fatal("selected accent")
			}
			for x := 1; x < w; x++ {
				c := out.Frame.Cell(x, y+i)
				if c.Width > 0 && !reflect.DeepEqual(c.Style.Bg, Surface) {
					t.Fatal("selected background", x)
				}
			}
		}
	}
}
