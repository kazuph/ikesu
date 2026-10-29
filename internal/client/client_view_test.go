package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
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
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{"p2"}) {
		t.Fatalf("selected tab: %+v", got)
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

func TestClientViewRejectsSnapshotFromPreviousConnection(t *testing.T) {
	e := &managedEndpoint{attempt: 2, snapshotAttempt: 1, connection: &endpointConnection{}, snapshot: &render.Input{State: model.New(t.TempDir(), 80, 24)}}
	m := &machineClient{session: "isolated", active: "local", endpoints: map[string]*managedEndpoint{"local": e}}
	got := m.currentClientView()
	if got.Session != "" || got.WorkspaceID != "" || len(got.PaneIDs) != 0 {
		t.Fatalf("stale reconnect snapshot: %+v", got)
	}
	e.snapshotAttempt = 2
	if got := m.currentClientView(); got.WorkspaceID != "s1" {
		t.Fatalf("current reconnect snapshot rejected: %+v", got)
	}
}

func TestClientViewFocusKeepsLastPaintedWorkspaceWhileNextPaintIsFenced(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	state.Apply(model.CmdNewWorkspace{})
	peer := endpoint.Current()
	c := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	e := &managedEndpoint{connection: c, snapshot: &render.Input{State: state}}
	m := &machineClient{session: "isolated", active: "local", focusKnown: true, endpoints: map[string]*managedEndpoint{"local": e}, clientViewConnection: c,
		lastClientView: &api.ClientView{MachineID: "local", Session: "isolated", WorkspaceID: "s1", PaneIDs: []string{"p1"}}}
	m.reportClientFocus()
	if len(c.queue) != 1 || c.queue[0].ClientView.WorkspaceID != "s1" || !*c.queue[0].ClientView.Focused {
		t.Fatalf("focus used an unpainted snapshot: %+v", c.queue)
	}
	m.reportClientView()
	if len(c.queue) != 2 || c.queue[1].ClientView.WorkspaceID != "s2" {
		t.Fatalf("paint did not publish the new workspace: %+v", c.queue)
	}
}

func TestClientViewUsesZoomOverlayAndPopupProjection(t *testing.T) {
	state := model.New(t.TempDir(), 100, 40)
	state.Apply(model.CmdSplitPane{Pane: 1})
	e := &managedEndpoint{connection: &endpointConnection{}, snapshot: &render.Input{State: state}}
	m := &machineClient{session: "isolated", active: "local", endpoints: map[string]*managedEndpoint{"local": e}}
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{"p1", "p2"}) {
		t.Fatalf("split: %+v", got)
	}
	state.ActiveWorkspace().TabRef().Zoomed = 2
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{"p2"}) {
		t.Fatalf("zoom: %+v", got)
	}
	state.ActiveWorkspace().TabRef().Zoomed = 0
	state.Apply(model.CmdOpenOverlay{TargetPaneID: 1})
	if got := m.currentClientView(); len(got.PaneIDs) != 2 || got.PaneIDs[0] == "p1" {
		t.Fatalf("covered target: %+v", got)
	}
	state.Apply(model.CmdCloseOverlay{})
	state.Apply(model.CmdOpenPopup{WorkspaceID: state.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 80}, Height: api.PopupSize{Cells: 20}})
	if got := m.currentClientView(); !reflect.DeepEqual(got.PaneIDs, []string{fmt.Sprintf("p%d", state.Popup.PaneID)}) {
		t.Fatalf("popup: %+v", got)
	}
}
