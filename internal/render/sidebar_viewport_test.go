package render

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"strings"
	"testing"
)

func TestG2ObservedMultipleCollapsedAndEmptySectionLines(t *testing.T) {
	s := model.New("/tmp/none", 120, 50)
	for _, sec := range []model.Section{model.SectionFavorite, model.SectionWork, model.SectionPersonal} {
		s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/" + string(sec), Section: sec, NoFocus: true})
	}
	check := func(expected map[int]string) {
		t.Helper()
		out := Compose(Input{State: s})
		for y, label := range expected {
			line := frameLine(out.Frame, y)
			if !strings.Contains(line, label) || !strings.Contains(line, "[new]") {
				t.Fatalf("row %d: %q", y, line)
			}
			if !strings.HasPrefix(frameLine(out.Frame, y-1), "────") || !strings.HasPrefix(frameLine(out.Frame, y+1), "────") {
				t.Fatalf("section lines at row %d", y)
			}
		}
		count := 0
		for y := 0; y < s.SidebarArea().H/2-1; y++ {
			if strings.Contains(frameLine(out.Frame, y), "[new]") {
				count++
			}
		}
		if count != len(expected) {
			t.Fatalf("header count %d", count)
		}
	}
	check(map[int]string{2: "▾ ⭐ favorites", 8: "▾ 💼 work", 14: "▾ 🏠 personal", 20: "▾ spaces"})
	s.Apply(model.CmdToggleSection{Section: model.SectionFavorite})
	check(map[int]string{2: "▸ ⭐ favorites", 5: "▾ 💼 work", 11: "▾ 🏠 personal", 17: "▾ spaces"})
	s.Apply(model.CmdCloseWorkspace{Index: 1})
	check(map[int]string{2: "▾ 💼 work", 8: "▾ 🏠 personal", 14: "▾ spaces"})
}

func TestG2PanelScrollTotalsCollapseAndFooterExclusion(t *testing.T) {
	s := model.New("/tmp", 100, 30)
	panes := map[int]*PaneSnap{}
	for i := 0; i < 20; i++ {
		if i > 0 {
			s.Apply(model.CmdNewWorkspace{Cwd: "/tmp", Section: model.SectionWork, NoFocus: true})
		}
		panes[i+1] = &PaneSnap{Agent: "codex"}
	}
	v := SidebarViewFor(s, panes, config.Config{}, 100, 100, false)
	if v.Workspace.Offset == 0 || v.Agents.Offset == 0 {
		t.Fatal("independent overflow missing", v)
	}
	for _, p := range []SidebarViewport{v.Workspace, v.Agents} {
		h, top := SidebarThumb(p)
		if top+h != p.Rect.H {
			t.Fatal("last scroll position", p, h, top)
		}
	}
	footer := SidebarWidthRect(s)
	for _, row := range v.Rows {
		if row.Row.Kind == RowAgent && row.Y >= footer.Y {
			t.Fatal("agent invaded footer", row)
		}
	}
	if v.Agents.Rect.Y+v.Agents.Rect.H > footer.Y {
		t.Fatal("scrollbar invaded footer")
	}
	s.Apply(model.CmdToggleSection{Section: model.SectionWork})
	next := SidebarViewFor(s, panes, config.Config{}, 100, 100, false)
	if next.Agents.Total != 1 || v.Workspace.Total-next.Workspace.Total != 19*3 {
		t.Fatal("collapsed cards still counted", v, next)
	}
}

func TestEndpointCollapsedSectionScrollStopsAfterWholeSlimCard(t *testing.T) {
	for _, slim := range []bool{false, true} {
		s := model.New(t.TempDir(), 160, 45)
		for _, sec := range []model.Section{model.SectionFavorite, model.SectionWork, model.SectionPersonal} {
			for i := 0; i < 2; i++ {
				s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), Section: sec, NoFocus: true})
			}
		}
		s.Apply(model.CmdToggleSection{Section: model.SectionFavorite})
		want := 6
		if slim {
			s.WorkspaceDensity = "slim"
			want = 2
		}
		v := SidebarViewFor(s, nil, config.Config{}, 100, 0, true)
		if v.Workspace.Offset != want {
			t.Fatalf("slim=%t: end offset %d want %d", slim, v.Workspace.Offset, want)
		}
		if slim {
			for _, pos := range v.Rows {
				if pos.Y == 7 && (pos.Row.Kind != RowWorkspace || pos.Row.Ws != 4) {
					t.Fatal("slim end begins inside a workspace card")
				}
			}
		}
	}
}

func TestLocalFullCardGitRowLeavesObservedFooterGap(t *testing.T) {
	for _, size := range []struct{ height, workspaces int }{{15, 1}, {25, 3}} {
		s := model.New(t.TempDir(), 100, size.height)
		for i := 1; i < size.workspaces; i++ {
			s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
		}
		v := SidebarViewFor(s, nil, config.Config{}, 0, 0, false)
		for _, row := range v.Rows {
			if row.Y == v.Workspace.Rect.Y+v.Workspace.Rect.H-1 && (row.Row.Kind == RowGit || row.Row.Kind == RowCardBlank) {
				t.Fatalf("height %d: full card painted the row immediately before the menu footer", size.height)
			}
		}
	}
}
