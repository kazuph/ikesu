package render

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestOyagoiSidebarPrecedesMachinesAndMovesWorkspace(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	remote := Input{ServerUI: true, State: testState(160, 45)}
	remote.State.Workspaces[0].Name = "HQ unique"
	remote.State.Workspaces[0].CustomName = true
	remote.State.Workspaces[0].Section = model.SectionFavorite
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}, {ID: "remote", Label: "Remote", Input: &remote}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: remote.State.Workspaces[0].ID, HideOriginal: true}}
	text := screenText(t, ComposeMachines(local, &m), 160, 45)
	if strings.Count(text, "🎏 HQ") != 1 || strings.Count(text, "HQ unique") != 1 || strings.Index(text, "🎏 HQ") > strings.Index(text, "Remote") {
		t.Fatalf("incorrect HQ placement:\n%s", text)
	}
	found := false
	for _, h := range m.Hits {
		if h.Machine == "remote" && h.Workspace == remote.State.Workspaces[0].ID && h.Row.Kind == RowWorkspace {
			found = true
		}
	}
	if !found {
		t.Fatal("HQ workspace has no hit target")
	}
}

func TestOyagoiOfflineWithoutSnapshotRemainsVisible(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}, {ID: "remote", Label: "Remote"}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: 4, Offline: true}}
	text := screenText(t, ComposeMachines(local, &m), 160, 45)
	if !strings.Contains(text, "(offline)") {
		t.Fatal("offline HQ missing")
	}
}

func TestOyagoiOfflineDoesNotHideSameNumberFromAnotherSession(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	local.State.Workspaces[0].Name = "other session"
	local.State.Workspaces[0].CustomName = true
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}}, Oyagoi: &Oyagoi{Machine: "local", Workspace: local.State.Workspaces[0].ID, Offline: true}}
	text := screenText(t, ComposeMachines(local, &m), 160, 45)
	if !strings.Contains(text, "other session") {
		t.Fatal("unrelated workspace hidden")
	}
}

func TestOyagoiHeadingIsNotAWorkspaceTargetAndOfflineHasUnsetTarget(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: 9, Offline: true}}
	out := ComposeMachines(local, &m)
	area := SidebarViewForInput(local).Workspace.Rect
	offline := false
	heading := false
	for _, hit := range m.Hits {
		if hit.Right {
			continue
		}
		line := ""
		for x := area.X; x < area.X+area.W; x++ {
			line += out.Frame.Cell(x, hit.Rect.Y).Content
		}
		if strings.Contains(line, "🎏 HQ") {
			heading = hit.OyagoiHeading && hit.Workspace == 0
		}
		if strings.Contains(line, "(offline)") {
			offline = hit.OfflineOyagoi && hit.Machine == "remote" && hit.Workspace == 9
		}
	}
	if !heading {
		t.Fatal("HQ heading missing toggle target")
	}
	if !offline {
		t.Fatal("offline HQ missing unset target")
	}
}

func TestOyagoiCollapsedHidesWorkspaceAndKeepsHeading(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	ws := local.State.Workspaces[0]
	ws.Name, ws.CustomName, ws.Section = "HQ unique", true, model.SectionFavorite
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}}, Oyagoi: &Oyagoi{Machine: "local", Workspace: ws.ID, HideOriginal: true, Collapsed: true}}
	text := screenText(t, ComposeMachines(local, &m), 160, 45)
	if !strings.Contains(text, "▸ 🎏 HQ") || strings.Contains(text, "HQ unique") {
		t.Fatalf("collapsed: %s", text)
	}
	m.Oyagoi.Collapsed = false
	text = screenText(t, ComposeMachines(local, &m), 160, 45)
	if !strings.Contains(text, "▾ 🎏 HQ") || strings.Count(text, "HQ unique") != 1 {
		t.Fatalf("expanded: %s", text)
	}
}

func TestMobileCollapsedOyagoiKeepsOtherSections(t *testing.T) {
	s := testState(40, 30)
	ws := s.Workspaces[0]
	rows := MobileRowsWithCollapsedOyagoi(s, nil, ws.ID, "", true, ws.Generation)
	heading := false
	for _, row := range rows {
		if row.Kind == RowMobileOyagoi {
			heading = true
		}
		if (row.Kind == RowWorkspace || row.Kind == RowGit) && row.Ws == 0 {
			t.Fatal("collapsed HQ row is visible")
		}
	}
	if !heading {
		t.Fatal("collapsed HQ heading is missing")
	}
	rows = MobileRowsWithCollapsedOyagoi(s, nil, 0, "foreign", true, "")
	if len(rows) == 0 || rows[0].Kind != RowMobileOyagoi {
		t.Fatal("foreign HQ heading missing")
	}
	for _, row := range rows {
		if row.Kind == RowMobileForeignOyagoi {
			t.Fatal("collapsed foreign row is visible")
		}
	}
}

func TestOyagoiAndFavoriteHeadingShareCellStyles(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		f := NewFrame(40, 2)
		drawSectionHeading(f, 0, 0, 20, "🎏 HQ", collapsed, false, false)
		drawSectionHeading(f, 0, 1, 20, "⭐ favorite", collapsed, false, true)
		if f.Cell(0, 0).Content != f.Cell(0, 1).Content || !f.Cell(0, 0).Style.Equal(&f.Cell(0, 1).Style) {
			t.Fatal("section arrows differ")
		}
		if !f.Cell(2, 0).Style.Equal(&f.Cell(2, 1).Style) {
			t.Fatal("section label styles differ")
		}
		if got := f.Cell(0, 0).Content; got != map[bool]string{true: "▸", false: "▾"}[collapsed] {
			t.Fatalf("arrow %q", got)
		}
	}
}

func TestOyagoiOfflineSuffixSurvivesLongMachineName(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}, {ID: "remote", Label: "a very long remote machine display name"}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: 4, Offline: true}}
	text := screenText(t, ComposeMachines(local, &m), 160, 45)
	if !strings.Contains(text, "(offline)") {
		t.Fatal("offline suffix was truncated")
	}
}
