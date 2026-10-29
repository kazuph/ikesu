package client

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"reflect"
	"strings"
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
	m.reportClientView(*e.snapshot)
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

func TestClientViewOnlyExcludesPaneCoveredByPaintedDialog(t *testing.T) {
	state := model.New(t.TempDir(), 160, 40)
	state.Apply(model.CmdSplitPane{Pane: 1, Dir: layout.Vertical, Ratio: 0.5})
	peer := endpoint.Current()
	c := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	in := render.Input{State: state, Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "A", Width: 1}}}}, 2: {Grid: [][]uv.Cell{{{Content: "B", Width: 1}}}}}}
	left := state.DisplayPaneRects()[0].Rect
	in.Menu = &render.Menu{X: left.X + 1, Y: left.Y + 1, Items: []render.MenuItem{{Label: "visible menu"}}}
	e := &managedEndpoint{connection: c, snapshot: &in}
	m := &machineClient{session: "isolated", active: "local", endpoints: map[string]*managedEndpoint{"local": e}, regions: render.ComposeClient(in, nil, "test", false)}
	if m.regions.MenuRect.X+m.regions.MenuRect.W > state.DisplayPaneRects()[1].Rect.X {
		t.Fatalf("menu must cover only left pane: %+v", m.regions.MenuRect)
	}
	m.reportClientView(in)
	if len(c.queue) != 1 || !reflect.DeepEqual(c.queue[0].ClientView.PaneIDs, []string{"p2"}) {
		t.Fatalf("uncovered pane: %+v", m.lastClientView)
	}
	in.Menu = nil
	m.regions = render.ComposeClient(in, nil, "test", false)
	in.Copy = &render.CopyMark{Pane: 1, Fullscreen: true}
	m.regions = render.ComposeClient(in, nil, "test", false)
	m.reportClientView(in)
	if !reflect.DeepEqual(m.lastClientView.PaneIDs, []string{"p1"}) {
		t.Fatalf("fullscreen copy: %+v", m.lastClientView)
	}
	var frame string
	for _, cell := range m.regions.Frame.Cells {
		frame += cell.Content
	}
	if !strings.Contains(frame, "A") || strings.Contains(frame, "B") {
		t.Fatalf("fullscreen rendered different membership: %s", frame)
	}
	state.Apply(model.CmdOpenPopup{WorkspaceID: state.ActiveWorkspace().ID, Width: api.PopupSize{Cells: 20}, Height: api.PopupSize{Cells: 10}})
	m.regions = render.ComposeClient(in, nil, "test", false)
	m.reportClientView(in)
	popupID := fmt.Sprintf("p%d", state.Popup.PaneID)
	if !reflect.DeepEqual(m.lastClientView.PaneIDs, []string{popupID}) {
		t.Fatalf("popup must cover fullscreen copy: %+v", m.lastClientView)
	}
	in.Dialog = &render.Dialog{Title: "over popup", Width: 40, CenterTerminal: true}
	m.regions = render.ComposeClient(in, nil, "test", false)
	m.reportClientView(in)
	if len(m.lastClientView.PaneIDs) != 0 {
		t.Fatalf("dialog must cover popup too: %+v", m.lastClientView)
	}
	in.Dialog = nil
	state.Apply(model.CmdClosePopup{})
	m.regions = render.ComposeClient(in, nil, "test", false)
	m.reportClientView(in)
	if !reflect.DeepEqual(m.lastClientView.PaneIDs, []string{"p1"}) {
		t.Fatalf("closing layers must restore copy pane: %+v", m.lastClientView)
	}

}
