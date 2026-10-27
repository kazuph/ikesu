package client

import (
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"reflect"
	"testing"
)

func TestClientViewUsesDisplayedMachineWorkspaceAndExplicitFocus(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	e := &managedEndpoint{profile: machine.Profile{ID: "mini", Session: "remote-session"}, connection: &endpointConnection{}, snapshot: &render.Input{State: state}}
	m := &machineClient{session: "local-session", active: "mini", endpoints: map[string]*managedEndpoint{"mini": e}}
	view := m.currentClientView()
	if view.Focused != nil || view.MachineID != "mini" || view.Session != "remote-session" || view.WorkspaceID != "s1" || !reflect.DeepEqual(view.PaneIDs, []string{"p1"}) {
		t.Fatalf("unknown remote view: %+v", view)
	}
	// Exercise real terminal focus decoding through the normal input handler.
	m.input(input.Event{Kind: input.KindFocus})
	if focus := m.currentClientView().Focused; focus == nil || !*focus {
		t.Fatal("focus report not retained")
	}
	m.input(input.Event{Kind: input.KindBlur})
	if focus := m.currentClientView().Focused; focus == nil || *focus {
		t.Fatal("blur report not retained")
	}
	e.connection = nil
	view = m.currentClientView()
	if view.Session != "" || view.WorkspaceID != "" || len(view.PaneIDs) != 0 {
		t.Fatalf("disconnected display claims membership: %+v", view)
	}
}

func TestClientViewRebuildsMembershipAcrossTabsAndWorkspaceMoves(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	state.Apply(model.CmdNewTab{Workspace: 0})
	e := &managedEndpoint{connection: &endpointConnection{}, snapshot: &render.Input{State: state}}
	m := &machineClient{session: "isolated", active: "local", endpoints: map[string]*managedEndpoint{"local": e}}
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{"p1", "p2"}) {
		t.Fatalf("all tabs: %+v", got)
	}
	state.Apply(model.CmdNewWorkspace{})
	if got := m.currentClientView(); got.WorkspaceID != "s2" || !reflect.DeepEqual(got.PaneIDs, []string{"p3"}) {
		t.Fatalf("switched space: %+v", got)
	}
	state.Apply(model.CmdMovePane{Pane: 1, Dest: model.MoveDest{Kind: model.MoveDestTab, TabW: 1, TabT: 0}})
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{"p1", "p3"}) {
		t.Fatalf("moved pane: %+v", got)
	}
}
