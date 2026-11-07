package render

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func TestTabStatusSharesSidebarPriorityGlyphColorAndSpinner(t *testing.T) {
	states := []StatusGlyph{
		{Key: "unknown", Rune: "·", Color: Dim},
		{Key: "idle", Rune: "○", Color: Green},
		{Key: "done", Rune: "●", Color: Peach},
		{Key: "working", Rune: "⠹", Color: Yellow},
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

func TestWorkspaceStatusUsesFirstTabAcrossSurfaces(t *testing.T) {
	states := []StatusGlyph{{Key: "idle", Rune: "○", Color: Green}, {Key: "done", Rune: "●", Color: Peach}, {Key: "unknown", Rune: "·", Color: Dim}}
	for _, first := range states {
		for _, later := range []StatusGlyph{{Key: "working", Rune: "⠹", Color: Yellow}, {Key: "blocked", Rune: "●", Color: Red}} {
			t.Run(first.Key+"/"+later.Key, func(t *testing.T) {
				s := model.New("/tmp", 160, 45)
				in := Input{State: s, Panes: map[int]*PaneSnap{1: {Status: first}}}
				s.Apply(model.CmdNewTab{Workspace: 0})
				in.Panes[s.FocusedPane()] = &PaneSnap{Status: later}
				want := first
				if first.Key == "unknown" {
					want = StatusGlyph{Rune: " ", Color: Dim}
				}
				if got := wsGlyph(s, 0, in); got != want {
					t.Fatalf("workspace=%+v want=%+v", got, want)
				}
				f := NewFrame(160, 45)
				drawSidebarRow(f, s, in, SidebarRow{Kind: RowWorkspace, Ws: 0}, 0, 0, 30, false)
				icon := first.Rune
				if c := f.Cell(1, 0); c.Content != icon || !reflect.DeepEqual(c.Style.Fg, f.ThemeColor(first.Color)) {
					t.Fatalf("sidebar=%+v", c)
				}
				f = NewFrame(160, 45)
				drawConfiguredSpace(f, s, in, SidebarRow{Kind: RowWorkspace, Ws: 0}, []config.SidebarToken{{Token: "state_icon"}, {Token: "state_text"}}, 0, 0, 30)
				line := frameLine(f, 0)
				if !strings.Contains(line, icon) || first.Key != "unknown" && !strings.Contains(line, first.Key) {
					t.Fatalf("configured=%q", line)
				}
				marks := map[string]string{"done": "🔵 ikesu", "idle": "ikesu", "unknown": "ikesu"}
				if got := WindowTitle(in, "", nil); got != marks[first.Key] {
					t.Fatalf("title=%q", got)
				}
				in.SidebarConfig.UI.WindowTitle = "plain"
				if got := WindowTitle(in, "", nil); got != "ikesu" {
					t.Fatalf("plain title=%q", got)
				}
				s.Cols, s.MobileWidthThreshold = 60, 64
				out := Compose(in)
				if c := out.Frame.Cell(1, 0); c.Content != icon || !reflect.DeepEqual(c.Style.Fg, out.Frame.ThemeColor(first.Color)) {
					t.Fatalf("mobile header=%+v", c)
				}
				in.MobileSwitcher = true
				out = Compose(in)
				found := false
				for y := 3; y < s.Rows; y++ {
					c := out.Frame.Cell(3, y)
					if c.Content == icon && reflect.DeepEqual(c.Style.Fg, out.Frame.ThemeColor(first.Color)) {
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

func TestWorkspaceStatusFollowsTabOrderAndClose(t *testing.T) {
	s := model.New("/tmp", 100, 25)
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Status: StatusGlyph{Key: "idle", Rune: "○", Color: Green}}}}
	s.Apply(model.CmdNewTab{Workspace: 0})
	in.Panes[s.FocusedPane()] = &PaneSnap{Status: StatusGlyph{Key: "blocked", Rune: "●", Color: Red}}
	if got := wsGlyph(s, 0, in).Key; got != "idle" {
		t.Fatal(got)
	}
	s.Apply(model.CmdMoveTab{Workspace: 0, Index: 1, InsertIndex: 0})
	if got := wsGlyph(s, 0, in).Key; got != "blocked" {
		t.Fatal(got)
	}
	s.Apply(model.CmdCloseTab{Workspace: 0, Tab: 0})
	if got := wsGlyph(s, 0, in).Key; got != "idle" {
		t.Fatal(got)
	}
}

func TestWorkspaceStatusForInactiveAndRemoteSpaces(t *testing.T) {
	makeInput := func() Input {
		s := model.New("/tmp", 160, 45)
		in := Input{State: s, Panes: map[int]*PaneSnap{1: {Status: StatusGlyph{Key: "done", Rune: "●", Color: Peach}}}}
		s.Apply(model.CmdNewTab{Workspace: 0})
		in.Panes[s.FocusedPane()] = &PaneSnap{Status: StatusGlyph{Key: "blocked", Rune: "●", Color: Red}}
		return in
	}
	local, remote := makeInput(), makeInput()
	local.State.Apply(model.CmdNewWorkspace{Cwd: "/tmp"})
	if got := wsGlyph(local.State, 0, local); got.Key != "done" {
		t.Fatalf("inactive=%+v", got)
	}
	machines := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}}
	out := ComposeMachines(local, &machines)
	for _, hit := range machines.Hits {
		if hit.Machine == "remote" && !hit.Header && !hit.New && hit.Row.Kind == RowWorkspace {
			cell := out.Frame.Cell(hit.Rect.X+1, hit.Rect.Y)
			if cell.Content != "●" || !reflect.DeepEqual(cell.Style.Fg, out.Frame.ThemeColor(Peach)) {
				t.Fatalf("remote=%+v", cell)
			}
			return
		}
	}
	t.Fatal("remote workspace not drawn")
}

func TestTabStatusScopesAggregationAndRetainsIconInNarrowRename(t *testing.T) {
	s := model.New("/tmp/repo", 80, 25)
	tab := s.ActiveWorkspace().TabRef()
	in := Input{State: s, Panes: map[int]*PaneSnap{1: {Status: StatusGlyph{Key: "working", Rune: "⠙", Color: Yellow}}}}
	s.Apply(model.CmdNewTab{Workspace: 0})
	other := s.ActiveWorkspace().TabRef()
	in.Panes[other.Layout.Focused()] = &PaneSnap{Status: StatusGlyph{Key: "blocked", Rune: "●", Color: Red}}
	if tabGlyph(tab, in).Key != "working" || wsGlyph(s, 0, in).Key != "working" {
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
