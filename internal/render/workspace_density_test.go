package render

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
)

func TestWorkspaceCardLinesFollowDensity(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		density string
		kinds   []SidebarRowKind
	}{
		{"full", []SidebarRowKind{RowWorkspace, RowGit, RowCardBlank}},
		{"slim", []SidebarRowKind{RowWorkspace, RowGit}},
		{"oneline", []SidebarRowKind{RowWorkspace}},
	} {
		s := testState(160, 45)
		s.WorkspaceDensity = c.density
		rows := WorkspaceCardRows(s, config.Config{}, 0)
		if len(rows) != len(c.kinds) {
			t.Fatalf("%s: rows=%v", c.density, rows)
		}
		for i, k := range c.kinds {
			if rows[i].Kind != k || rows[i].Ws != 0 {
				t.Fatalf("%s: row %d = %+v", c.density, i, rows[i])
			}
		}
		if got := WorkspaceCardHeight(c.density); got != len(c.kinds) {
			t.Fatalf("%s: wheel step %d", c.density, got)
		}
	}
	if NextWorkspaceDensity("full") != "slim" || NextWorkspaceDensity("slim") != "oneline" || NextWorkspaceDensity("oneline") != "full" {
		t.Fatal("density button must cycle full → slim → oneline → full")
	}
	if WorkspaceDensityLabel("oneline") != "[oneline]" || WorkspaceDensityLabel("") != "[full]" {
		t.Fatal("density label")
	}
}

func TestWorkspaceCardShowsTabNamesBeforeGitAndPreview(t *testing.T) {
	t.Parallel()
	s := testState(160, 45)
	s.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
	s.Workspaces[0].Tabs[0].Name = "boss"
	s.Workspaces[0].Tabs[1].Name = "impl"
	in := Input{State: s, Git: map[int]GitStatus{0: {Branch: "main"}}, WorkspacePreviews: map[int]string{s.Workspaces[0].ID: "Worked for 45s"}}
	out := Compose(in)
	text := screenText(t, out, 160, 45)
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "boss") {
			line = l
		}
	}
	if b, i, m := strings.Index(line, "boss"), strings.Index(line, "impl"), strings.Index(line, "main"); b < 0 || i < b || m < i {
		t.Fatalf("tab chips must come before git on the card's second line: %q\n%s", line, text)
	}
	for x := 0; x < s.SidebarArea().W; x++ {
		for y := 0; y < 45; y++ {
			if c := out.Frame.Cell(x, y); (c.Content == "b" || c.Content == "i") && c.Style.Bg == out.Frame.Palette.Accent {
				t.Fatal("tab names have no fill")
			}
		}
	}
	if !strings.Contains(text, "Worked for 45s") {
		t.Fatalf("full density must show the first pane's preview line:\n%s", text)
	}
	s.WorkspaceDensity = "slim"
	if strings.Contains(screenText(t, Compose(in), 160, 45), "Worked for 45s") {
		t.Fatal("slim density has no preview line")
	}
	s.WorkspaceDensity = "oneline"
	out = Compose(in)
	sidebar := ""
	for y := 0; y < 45; y++ {
		for x := 0; x < s.SidebarArea().W; x++ {
			sidebar += out.Frame.Cell(x, y).Content
		}
		sidebar += "\n"
	}
	if text := screenText(t, out, 160, 45); strings.Contains(sidebar, "boss") || strings.Contains(text, "main") || strings.Contains(text, "Worked for 45s") {
		t.Fatalf("oneline density shows only the name:\n%s", text)
	}
}

func TestHQCardHasTheSameLinesAsItsSection(t *testing.T) {
	t.Parallel()
	for _, density := range []string{"full", "slim", "oneline"} {
		local := Input{ServerUI: true, State: testState(160, 45)}
		remote := Input{ServerUI: true, State: testState(160, 45), Git: map[int]GitStatus{0: {Branch: "hqbranch"}}}
		remote.State.WorkspaceDensity = density
		m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}, {ID: "remote", Label: "Remote", Input: &remote}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: remote.State.Workspaces[0].ID, HideOriginal: true}}
		text := screenText(t, ComposeMachines(local, &m), 160, 45)
		var kinds []SidebarRowKind
		for _, h := range m.Hits {
			if h.Machine == "remote" && h.Oyagoi && !h.OyagoiHeading {
				kinds = append(kinds, h.Row.Kind)
			}
		}
		want := len(WorkspaceCardRows(remote.State, config.Config{}, 0))
		if len(kinds) != want {
			t.Fatalf("%s: HQ card rows %v want %d\n%s", density, kinds, want, text)
		}
		if (density != "oneline") != strings.Contains(text, "hqbranch") {
			t.Fatalf("%s: HQ git line\n%s", density, text)
		}
	}
}

func TestLinkedWorktreeCardStaysOneLine(t *testing.T) {
	t.Parallel()
	s := testState(160, 45)
	s.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoKey: "/r/.git", RepoRoot: "/r", CheckoutPath: "/r/.worktree/x", IsLinkedWorktree: true}
	if rows := WorkspaceCardRows(s, config.Config{}, 0); len(rows) != 1 {
		t.Fatalf("rows=%v", rows)
	}
}
