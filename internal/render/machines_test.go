package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

func TestMachineSidebarKeepsOriginalIDsAndOnlySelectedEndpointAccent(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	for i, state := range []*Input{{State: localState}, {State: remoteState}} {
		state.State.Workspaces[0].ID = 1
		state.State.Workspaces[0].Name = []string{"Local real", "Remote real"}[i]
		state.State.Workspaces[0].CustomName = true
	}
	local := Input{ServerUI: true, State: localState}
	remote := Input{ServerUI: true, State: remoteState}
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Label: "Local", Status: "online", Input: &local}, {ID: "m-original", Label: "Mini test", Status: "online", Input: &remote}}, Active: "local"}
	for _, selected := range []string{"local", "m-original"} {
		machines.Active = selected
		active := local
		if selected != "local" {
			active = remote
		}
		out := ComposeMachines(active, &machines)
		text := screenText(t, out, 160, 45)
		for _, want := range []string{"Local real", "Remote real", "▾ Mini test", "Local: online", "Mini test: online"} {
			if !strings.Contains(text, want) {
				t.Fatal("missing machine/sidebar content", want)
			}
		}
		found := map[string]MachineHit{}
		for _, hit := range machines.Hits {
			if hit.Row.Kind == RowWorkspace && hit.Workspace != 0 {
				found[hit.Machine] = hit
			}
		}
		if len(found) != 2 {
			t.Fatal("machine workspace hit targets missing", found)
		}
		for machine, hit := range found {
			if hit.Workspace != 1 {
				t.Fatal("machine workspace ID was rewritten", hit)
			}
			cell := out.Frame.Cell(hit.Rect.X, hit.Rect.Y)
			if (cell.Content == "▌") != (machine == selected) {
				t.Fatal("accent leaked across endpoints", machine, selected, cell)
			}
		}
	}
	if localState.Active != 0 || remoteState.Active != 0 {
		t.Fatal("composition changed endpoint navigation state")
	}
	plain := Compose(local)
	without := ComposeMachines(local, nil)
	if !reflect.DeepEqual(plain, without) {
		t.Fatal("no-machine composition changed")
	}
}

func TestMachineSidebarDropIndicatorUsesRemoteGroupCoordinates(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	remoteState.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	local, remote := Input{ServerUI: true, State: localState}, Input{ServerUI: true, State: remoteState}
	drop := 2
	remote.Dragging, remote.DraggedWorkspace, remote.DropIndex = true, 0, &drop
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}, Active: "remote"}
	out := ComposeMachines(remote, &machines)
	last := -1
	for _, hit := range machines.Hits {
		if hit.Machine == "remote" && hit.Row.Ws == 1 && (hit.Row.Kind == RowWorkspace || hit.Row.Kind == RowGit || hit.Row.Kind == RowCardBlank) {
			last = max(last, hit.Rect.Y)
		}
	}
	if last < 0 {
		t.Fatal("remote card hit area missing")
	}
	area := SidebarViewForInput(remote).Workspace.Rect
	for x := area.X; x < area.X+area.W; x++ {
		cell := out.Frame.Cell(x, last+1)
		if cell.Content != "─" || !reflect.DeepEqual(cell.Style.Fg, out.Frame.Palette.Accent) {
			t.Fatal("remote insertion line missing", x, last+1, cell)
		}
	}
	machines.Active = "local"
	out = ComposeMachines(local, &machines)
	if out.Frame.Cell(area.X, last+1).Content == "─" {
		t.Fatal("hidden remote drag leaked into Local")
	}
	machines.DropMachine, machines.DropIndex = "remote", &drop
	out = ComposeMachines(local, &machines)
	if out.Frame.Cell(area.X, last+1).Content != "─" {
		t.Fatal("pending remote drop indicator was not drawn while Local remains selected")
	}
}

func TestPendingMachineSectionHighlightsOnlyItsTargetWithoutSelectingRemote(t *testing.T) {
	localState, remoteState := testState(160, 45), testState(160, 45)
	remoteState.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true, Section: model.SectionWork})
	local, remote := Input{ServerUI: true, State: localState}, Input{ServerUI: true, State: remoteState}
	machines := MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}}, Active: "local", DropMachine: "remote", DropSection: model.SectionWork}
	out := ComposeMachines(local, &machines)
	found := false
	for _, hit := range machines.Hits {
		if hit.Machine == "remote" && hit.Row.Kind == RowSection && hit.Row.Section == model.SectionWork {
			found = true
			for x := hit.Rect.X; x < hit.Rect.X+hit.Rect.W; x++ {
				cell := out.Frame.Cell(x, hit.Rect.Y)
				if cell.Width == 0 {
					previous := out.Frame.Cell(x-1, hit.Rect.Y)
					if previous == nil || previous.Width != 2 {
						t.Fatal("section header has an orphaned wide-character continuation", x)
					}
					cell = previous
				}
				if !reflect.DeepEqual(cell.Style.Bg, out.Frame.Palette.Accent) {
					t.Fatal("pending section target is not highlighted across its header", x)
				}
			}
		}
		if hit.Machine == "remote" && hit.Workspace != 0 && out.Frame.Cell(hit.Rect.X, hit.Rect.Y).Content == "▌" {
			t.Fatal("pending section move selected a hidden Remote card")
		}
	}
	if !found || remote.DragSection != "" || localState.Active != 0 || remoteState.Active != 0 {
		t.Fatal("missing target or composition mutated endpoint navigation")
	}
}
