package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestG2SectionsRowsCollapseAndAgents(t *testing.T) {
	s := model.New("/tmp/one", 100, 40)
	s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/two", Section: model.SectionWork})
	s.Apply(model.CmdSetSection{Index: 0, Section: model.SectionFavorite})
	panes := map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Key: "working"}}, 2: {Agent: "claude", Status: StatusGlyph{Key: "blocked"}}}
	rows := SidebarRows(s, panes)
	var secs []model.Section
	for _, r := range rows {
		if r.Kind == RowSection {
			secs = append(secs, r.Section)
		}
	}
	if !reflect.DeepEqual(secs, []model.Section{model.SectionFavorite, model.SectionWork}) {
		t.Fatal(secs)
	}
	s.Apply(model.CmdToggleSection{Section: model.SectionFavorite})
	for _, r := range SidebarRows(s, panes) {
		if (r.Kind == RowWorkspace || r.Kind == RowAgent) && r.Ws == 0 {
			t.Fatal("collapsed row", r)
		}
	}
}
func TestG2SlimRowsHighlightAndFixedTarget(t *testing.T) {
	s := model.New("/tmp/long-workspace-name", 90, 30)
	s.WorkspaceDensity = "slim"
	panes := map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Key: "working", Rune: "◐", Color: Yellow}}}
	out := Compose(Input{State: s, Panes: panes, Git: map[int]GitStatus{0: {Branch: "main", Ahead: 2, Behind: 1, Added: 123, Deleted: 11}}})
	rows := SidebarRows(s, panes)
	_, iy, _, _ := SidebarContent(s.SidebarArea())
	var name, git, agent int
	for i, r := range rows {
		switch r.Kind {
		case RowWorkspace:
			name = iy + i
		case RowGit:
			git = iy + i
		case RowAgent:
			agent = iy + i
		}
	}
	if git != name+1 {
		t.Fatal(name, git)
	}
	for _, y := range []int{name, git} {
		c := out.Frame.Cell(0, y)
		if c.Content != "▌" || !reflect.DeepEqual(c.Style.Fg, Accent) {
			t.Fatal("accent", y, c)
		}
	}
	if !strings.Contains(frameLine(out.Frame, agent), "%1") {
		t.Fatal(frameLine(out.Frame, agent))
	}
	if !strings.Contains(frameLine(out.Frame, git), "↑2 ↓1 +123 -11") {
		t.Fatal(frameLine(out.Frame, git))
	}
}
func frameLine(f *Frame, y int) string {
	var b strings.Builder
	for x := 0; x < f.W; x++ {
		c := f.Cell(x, y)
		if c != nil && c.Width > 0 {
			b.WriteString(c.Content)
		}
	}
	return b.String()
}
func TestG2GitPriorityZeroAndColors(t *testing.T) {
	g := GitStatus{Branch: "a-very-long-branch-name", Ahead: 2, Behind: 1, Added: 203, Deleted: 31}
	f := NewFrame(24, 1)
	drawGitRow(f, 0, 0, 24, g, uv.Style{})
	line := frameLine(f, 0)
	if !strings.HasPrefix(line, "↑2 ↓1 +203 -31 ") || !strings.Contains(line, "…") {
		t.Fatal(line)
	}
	if !reflect.DeepEqual(f.Cell(0, 0).Style.Fg, Green) || !reflect.DeepEqual(f.Cell(3, 0).Style.Fg, Red) {
		t.Fatal("colors")
	}
	if strings.Contains(GitLabel(GitStatus{Branch: "main"}, 20), "+0") {
		t.Fatal("zero label")
	}
}
func TestG2DisplayNameAgentOnlyAndBudget(t *testing.T) {
	s := model.New("/tmp/pion", 100, 30)
	w := s.Workspaces[0]
	p := map[int]*PaneSnap{1: {Title: "kazuph@host:pion"}}
	if WorkspaceDisplayName(w, p) != "pion" {
		t.Fatal("shell title")
	}
	p[1].Agent = "codex"
	p[1].Title = "planner"
	if WorkspaceDisplayName(w, p) != "pion-planner" {
		t.Fatal("agent title")
	}
	got := BudgetLabels([]string{"longworkspace", "longtab", "codex"}, 18)
	if !strings.Contains(got, " · ") {
		t.Fatal(got)
	}
}
func TestG2SectionNewNarrowAndWidthFooter(t *testing.T) {
	for w := 0; w < 10; w++ {
		r := SectionNewRect(0, 0, w)
		if (r.W > 0) != (w >= 6) {
			t.Fatal(w, r)
		}
	}
	s := model.New("", 100, 30)
	r := SidebarWidthRect(s)
	if r.W != 8 {
		t.Fatal(r)
	}
	s.Cols = 7
	if SidebarWidthRect(s).W != 0 {
		t.Fatal("invisible hit")
	}
}

func TestG2DropTargetWholeHeaderAndBottomIndicator(t *testing.T) {
	s := model.New("/tmp", 100, 30)
	s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/two", Section: model.SectionWork})
	out := Compose(Input{State: s, DragSection: model.SectionWork, Dragging: true, DraggedWorkspace: 0})
	rows := SidebarRows(s, nil)
	ix, y, w, _ := SidebarContent(s.SidebarArea())
	for i, r := range rows {
		if r.Kind == RowSection && r.Section == model.SectionWork {
			for x := ix; x < ix+w; x++ {
				if out.Frame.Cell(x, y+i).Width > 0 && !reflect.DeepEqual(out.Frame.Cell(x, y+i).Style.Bg, Accent) {
					t.Fatal("header background", x)
				}
			}
			if out.Frame.Cell(ix, y+i).Style.Attrs&uv.AttrBold == 0 {
				t.Fatal("header not bold")
			}
		}
	}
	drop := 2
	out = Compose(Input{State: s, DropIndex: &drop, Dragging: true, DraggedWorkspace: 1})
	last := -1
	for i, r := range rows {
		if r.Ws == 1 && (r.Kind == RowWorkspace || r.Kind == RowGit) {
			last = i
		}
	}
	for x := ix; x < ix+w; x++ {
		if out.Frame.Cell(x, y+last+1).Content != "─" {
			t.Fatal("missing bottom insertion indicator", x)
		}
	}
}

func TestG2WorkspaceNumberAndGlobalPane22(t *testing.T) {
	s := model.New("/tmp/one", 100, 40)
	for i := 1; i < 22; i++ {
		s.Apply(model.CmdSplitFocused{Dir: 0})
	}
	panes := map[int]*PaneSnap{22: {Agent: "long-agent-label", Status: StatusGlyph{Rune: "·", Color: Dim}}}
	out := Compose(Input{State: s, Panes: panes})
	rows := SidebarRows(s, panes)
	_, y, _, _ := SidebarContent(s.SidebarArea())
	for i, r := range rows {
		line := frameLine(out.Frame, y+i)
		if r.Kind == RowWorkspace && !strings.Contains(line, "· 1 one") {
			t.Fatal(line)
		}
		if r.Kind == RowAgent && (!strings.Contains(line, "%22") || strings.Contains(line, "1-1") || strings.Contains(line, "2-1")) {
			t.Fatal(line)
		}
	}
}
