package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
	"reflect"
	"strings"
	"testing"
)

func TestG2TokenStylesContextAndOccurrence(t *testing.T) {
	off, on := false, true
	fg := "#abc"
	base := uv.Style{Fg: Green, Bg: Surface, Attrs: uv.AttrBold}
	if got := tokenStyle(config.SidebarToken{Token: "workspace"}, base); !reflect.DeepEqual(got, base) {
		t.Fatal("default changed")
	}
	got := tokenStyle(config.SidebarToken{Token: "workspace", Fg: &fg, Bold: &off, Dim: &on}, base)
	if !reflect.DeepEqual(got.Fg, color.RGBA{0xaa, 0xbb, 0xcc, 255}) || got.Attrs&uv.AttrBold != 0 || got.Attrs&uv.AttrFaint == 0 || got.Bg != base.Bg {
		t.Fatal(got)
	}
	s := model.New("/tmp/project", 100, 30)
	panes := map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Key: "working", Rune: "◐", Color: Yellow}}}
	cfg := config.Config{}
	cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace", Fg: &fg, Bold: &on}, {Token: "workspace"}}}
	out := Compose(Input{State: s, Panes: panes, SidebarConfig: cfg})
	rows := SidebarRows(s, panes, cfg)
	_, y, _, _ := SidebarContent(s.SidebarArea())
	for _, r := range rows {
		if r.Kind == RowAgent {
			y = s.SidebarArea().Y + (s.SidebarArea().H+1)/2 + 2
			line := frameLine(out.Frame, y)
			if !strings.Contains(line, "%1") {
				t.Fatal("fixed target missing", line)
			}
			first := strings.Index(line, "project")
			second := strings.LastIndex(line, "project")
			if first == second || !reflect.DeepEqual(out.Frame.Cell(first, y).Style.Fg, color.RGBA{0xaa, 0xbb, 0xcc, 255}) || !reflect.DeepEqual(out.Frame.Cell(second, y).Style.Fg, Text) {
				t.Fatal("occurrence styles", line)
			}
		}
	}
}
func TestG2AgentStableSortAndMinimumBudget(t *testing.T) {
	s := model.New("/tmp", 100, 40)
	for i := 0; i < 4; i++ {
		s.Apply(model.CmdSplitFocused{Dir: 0})
	}
	panes := map[int]*PaneSnap{}
	for id, key := range []string{"unknown", "idle", "working", "done", "blocked"} {
		panes[id+1] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: key}}
	}
	s.AgentScope = "sort"
	var got []int
	for _, r := range SidebarRows(s, panes) {
		if r.Kind == RowAgent {
			got = append(got, r.Pane)
		}
	}
	if !reflect.DeepEqual(got, []int{5, 4, 3, 2, 1}) {
		t.Fatal(got)
	}
	for width := 1; width < 40; width++ {
		f := NewFrame(width, 1)
		drawTokens(f, 0, 0, width, []styledToken{{name: "pane", text: "%22", fixed: true}, {name: "workspace", text: "longworkspace"}, {name: "tab", text: "longtab"}, {name: "agent", text: "codex"}})
		if width >= 3 && !strings.Contains(frameLine(f, 0), "%22") {
			t.Fatal(width, frameLine(f, 0))
		}
	}
}
