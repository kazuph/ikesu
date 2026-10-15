package render

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestTabStatusSharesSidebarPriorityGlyphColorAndSpinner(t *testing.T) {
	states := []StatusGlyph{
		{Key: "unknown", Rune: "·", Color: Dim},
		{Key: "idle", Rune: "○", Color: Green},
		{Key: "working", Rune: "⠹", Color: Yellow},
		{Key: "done", Rune: "●", Color: Peach},
		{Key: "blocked", Rune: "●", Color: Red},
	}
	for _, server := range []bool{false, true} {
		for i, first := range states {
			for j, second := range states {
				t.Run(fmt.Sprintf("server=%v/%s/%s", server, first.Key, second.Key), func(t *testing.T) {
					s := model.New("/tmp/repo", 160, 35)
					tab := s.ActiveWorkspace().TabRef()
					tab.Name = "日本語"
					tab.Layout.SplitFocused(layout.Vertical, 2)
					in := Input{State: s, ServerUI: server, Panes: map[int]*PaneSnap{1: {Status: first}, 2: {Status: second}}}
					want := states[max(i, j)]
					if i == 0 && j == 0 {
						want = StatusGlyph{Rune: " ", Color: Dim}
					}
					if got := tabGlyph(tab, in); got != want || got != wsGlyph(s, 0, in) {
						t.Fatalf("aggregate=%+v want=%+v sidebar=%+v", got, want, wsGlyph(s, 0, in))
					}
					out := Compose(in)
					r := out.TabHits[0]
					glyph := out.Frame.Cell(r.X+1, r.Y)
					rune := want.Rune
					if rune == " " {
						rune = "·"
					}
					if glyph.Content != rune || !reflect.DeepEqual(glyph.Style.Fg, out.Frame.ThemeColor(want.Color)) {
						t.Fatalf("glyph=%+v want=%+v", glyph, want)
					}
					if r.W != 12 || out.Frame.Cell(r.X+3, r.Y).Content != "日" || tab.Name != "日本語" {
						t.Fatal("name, width or API label changed", r, tab.Name)
					}
				})
			}
		}
	}
}

func TestTabStatusScopesAggregationAndRetainsIconInNarrowRename(t *testing.T) {
	s := model.New("/tmp/repo", 80, 25)
	tab := s.ActiveWorkspace().TabRef()
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Status: StatusGlyph{Key: "working", Rune: "⠙", Color: Yellow}}}}
	s.Apply(model.CmdNewTab{Workspace: 0})
	other := s.ActiveWorkspace().TabRef()
	in.Panes[other.Layout.Focused()] = &PaneSnap{Status: StatusGlyph{Key: "blocked", Rune: "●", Color: Red}}
	if tabGlyph(tab, in).Key != "working" || wsGlyph(s, 0, in).Key != "blocked" {
		t.Fatal("tab aggregation leaked")
	}
	for _, server := range []bool{false, true} {
		in.ServerUI = server
		for _, cols := range []int{80, 60, 45} {
			s.Cols = cols
			s.MobileWidthThreshold = 0
			s.ActiveWorkspace().ActiveTab = 0
			for _, name := range []string{"x", "日本語の長いタブ名とパス/filename.go"} {
				tab.Name = name
				out := Compose(in)
				r := out.TabHits[0]
				if r.W < 3 || out.Frame.Cell(r.X+1, r.Y).Content != "⠙" || tab.Name != name {
					t.Fatalf("server=%v cols=%d rect=%+v name=%s", server, cols, r, name)
				}
			}
		}
	}
}

func TestTabStatusMissingPaneAndAgentlessDecisionGlyph(t *testing.T) {
	s := model.New("/tmp", 100, 25)
	tab := s.ActiveWorkspace().TabRef()
	for _, panes := range []map[int]*PaneSnap{nil, {1: {}}, {1: {Status: StatusGlyph{Key: "blocked", Rune: "●", Color: Red}}}} {
		in := Input{State: s, Panes: panes}
		got := tabGlyph(tab, in)
		if got != wsGlyph(s, 0, in) {
			t.Fatal("shell attention differs from sidebar")
		}
		if panes[1] != nil && panes[1].Status.Key == "blocked" && got.Key != "blocked" {
			t.Fatal("agentless decision ignored")
		}
	}
}
