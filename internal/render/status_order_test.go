package render

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/agentview"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestStatusOrderWorkspaceSurfaces(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, density := range []string{"slim", "full"} {
			for _, keys := range [][3]string{{"working", "done", "working"}, {"blocked", "working", "blocked"}, {"done", "idle", "done"}, {"idle", "unknown", "idle"}} {
				t.Run(fmt.Sprint(server, density, keys), func(t *testing.T) {
					s := model.New("/tmp", 160, 45)
					s.WorkspaceDensity = density
					s.ActiveWorkspace().TabRef().Layout.SplitFocused(layout.Vertical, 2)
					glyphs := map[string]StatusGlyph{"working": {Key: "working", Rune: "⠹", Color: Yellow}, "done": {Key: "done", Rune: "●", Color: Peach}, "blocked": {Key: "blocked", Rune: "●", Color: Red}, "idle": {Key: "idle", Rune: "○", Color: Green}, "unknown": {Key: "unknown", Rune: "·", Color: Dim}}
					in := Input{State: s, ServerUI: server, Panes: map[int]*PaneSnap{1: {Status: glyphs[keys[0]]}, 2: {Status: glyphs[keys[1]]}}}
					want := glyphs[keys[2]]
					check := func(f *Frame, x, y int) {
						t.Helper()
						c := f.Cell(x, y)
						if c.Content != want.Rune || !reflect.DeepEqual(c.Style.Fg, f.ThemeColor(want.Color)) {
							t.Fatalf("cell(%d,%d)=%+v want=%+v", x, y, c, want)
						}
					}
					f := NewFrame(160, 45)
					drawSidebarRow(f, s, in, SidebarRow{Kind: RowWorkspace, Ws: 0}, 0, 0, 30)
					check(f, 1, 0)
					f = NewFrame(160, 45)
					drawSidebarCollapsed(f, s, in, layout.Rect{W: 2, H: 40})
					check(f, 0, 0)
					s.Cols = 60
					s.MobileWidthThreshold = 64
					in.MobileSwitcher = true
					out := Compose(in)
					found := false
					for y := 0; y < s.Rows; y++ {
						c := out.Frame.Cell(3, y)
						if c.Content == want.Rune && reflect.DeepEqual(c.Style.Fg, out.Frame.ThemeColor(want.Color)) {
							found = true
						}
					}
					if !found {
						t.Fatal("mobile workspace glyph missing")
					}
				})
			}
		}
	}
}

func TestStatusOrderAgentPriorityAndAttentionView(t *testing.T) {
	s := model.New("/tmp", 160, 45)
	for i := 0; i < 4; i++ {
		s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	}
	panes := map[int]*PaneSnap{}
	for i, key := range []string{"unknown", "idle", "done", "working", "blocked"} {
		panes[i+1] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: key}}
	}
	s.AgentScope = "sort"
	cfg := config.Config{}
	cfg.UI.AgentPanelSort = "priority"
	for _, view := range []*agentview.View{nil, {Sort: []agentview.Sort{{Field: []byte(`"attention"`)}}}} {
		s.AgentView = view
		var got []int
		for _, r := range SidebarRows(s, panes, cfg) {
			if r.Kind == RowAgent && r.Line == 0 {
				got = append(got, r.Pane)
			}
		}
		if !reflect.DeepEqual(got, []int{5, 4, 3, 2, 1}) {
			t.Fatal(got)
		}
	}
}
