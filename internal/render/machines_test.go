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
