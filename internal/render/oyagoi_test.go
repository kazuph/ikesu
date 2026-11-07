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
	if strings.Count(text, "🎏 oyagoi") != 1 || strings.Count(text, "HQ unique") != 1 || strings.Index(text, "🎏 oyagoi") > strings.Index(text, "Remote") {
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

func TestOyagoiHeadingAndOfflineLabelAreNotWorkspaceTargets(t *testing.T) {
	local := Input{ServerUI: true, State: testState(160, 45)}
	m := MachineSidebar{Active: "local", Groups: []MachineGroup{{ID: "local", Label: "Local", Input: &local}}, Oyagoi: &Oyagoi{Machine: "remote", Workspace: 9, Offline: true}}
	out := ComposeMachines(local, &m)
	area := SidebarViewForInput(local).Workspace.Rect
	for _, hit := range m.Hits {
		if hit.Right {
			continue
		}
		line := ""
		for x := area.X; x < area.X+area.W; x++ {
			line += out.Frame.Cell(x, hit.Rect.Y).Content
		}
		if strings.Contains(line, "oyagoi") || strings.Contains(line, "(offline)") {
			t.Fatalf("heading or offline label was clickable: %+v", hit)
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
