package render

import (
	"reflect"
	"strings"
	"testing"
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
