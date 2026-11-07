package client

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestOyagoiActionsUseMachineSessionAndWorkspaceIdentity(t *testing.T) {
	s := model.New(t.TempDir(), 80, 24)
	ws := s.Workspaces[0]
	store := oyagoiStore{filepath.Join(t.TempDir(), "oyagoi.json")}
	m := &machineClient{session: "one", oyagoiStore: store, endpoints: map[string]*managedEndpoint{
		"local":  {profile: machine.Profile{ID: "local", Session: "one"}, snapshot: &render.Input{State: s}},
		"remote": {profile: machine.Profile{ID: "remote", Session: "two"}, snapshot: &render.Input{State: s}},
	}}
	set := fmt.Sprintf("oyagoi:set:%d:%s", ws.ID, ws.Generation)
	unset := fmt.Sprintf("oyagoi:unset:%d:%s", ws.ID, ws.Generation)
	if err := m.oyagoiAction("local", set); err != nil {
		t.Fatal(err)
	}
	if err := m.oyagoiAction("remote", set); err != nil {
		t.Fatal(err)
	}
	if err := m.oyagoiAction("local", unset); err != nil {
		t.Fatal(err)
	}
	got, err := store.load()
	if err != nil || got == nil || got.Machine != "remote" || got.Session != "two" || got.Workspace != ws.ID || got.Generation != ws.Generation {
		t.Fatalf("selection = %+v, %v", got, err)
	}
	if err := m.oyagoiAction("remote", unset); err != nil {
		t.Fatal(err)
	}
	if got, err := store.load(); err != nil || got != nil {
		t.Fatalf("unset = %+v, %v", got, err)
	}
}

func TestOyagoiMenuRequestCarriesWorkspaceIdentity(t *testing.T) {
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{oyagoi: &oyagoiSelection{Machine: "local", Session: "one", Workspace: 4, Generation: "gen"}, endpoints: map[string]*managedEndpoint{"local": {connection: conn}}}
	if err := m.openOyagoiMenu("local", 3, 5); err != nil {
		t.Fatal(err)
	}
	if len(conn.queue) != 1 || conn.queue[0].Type != "workspace_menu" || conn.queue[0].Workspace != 4 || conn.queue[0].WorkspaceGeneration != "gen" {
		t.Fatalf("request = %+v", conn.queue)
	}
}

func TestOyagoiProjectionTargetsOnlyMatchingSession(t *testing.T) {
	peer := endpoint.Current()
	local := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	remote := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "remote", Session: "two", Workspace: 4, Generation: "gen"}, endpoints: map[string]*managedEndpoint{"local": {connection: local}, "remote": {connection: remote, profile: machine.Profile{Session: "two"}}}}
	if err := m.syncOyagoi("local"); err != nil {
		t.Fatal(err)
	}
	if err := m.syncOyagoi("remote"); err != nil {
		t.Fatal(err)
	}
	if len(local.queue) != 1 || local.queue[0].Workspace != 0 || local.queue[0].OyagoiLabel == "" || len(remote.queue) != 1 || remote.queue[0].Type != "oyagoi_view" || remote.queue[0].Workspace != 4 || remote.queue[0].WorkspaceGeneration != "gen" {
		t.Fatalf("local=%+v remote=%+v", local.queue, remote.queue)
	}
}

func TestOyagoiSelectOtherMachineRequestsWorkspace(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	ws := state.Workspaces[0]
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "remote", Session: "two", Workspace: ws.ID, Generation: ws.Generation}, endpoints: map[string]*managedEndpoint{"remote": {connection: conn, profile: machine.Profile{Session: "two"}, snapshot: &render.Input{State: state}}}}
	if err := m.selectOyagoi(); err != nil {
		t.Fatal(err)
	}
	if m.pending == nil || m.pending.id != "remote" || m.pending.workspace != ws.ID {
		t.Fatalf("pending = %+v", m.pending)
	}
}

func TestOyagoiRejectsSnapshotFromPreviousConnectionReview(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	ws := state.Workspaces[0]
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "remote", Session: "two", Workspace: ws.ID, Generation: ws.Generation}, endpoints: map[string]*managedEndpoint{"remote": {connection: conn, profile: machine.Profile{Session: "two"}, snapshot: &render.Input{State: state}, attempt: 2, snapshotAttempt: 1}}}
	if err := m.selectOyagoi(); err == nil || m.pending != nil {
		t.Fatal("stale snapshot accepted")
	}
}

func TestOyagoiSelectionRequestIncludesGenerationReview(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	ws := state.Workspaces[0]
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "remote", Session: "two", Workspace: ws.ID, Generation: ws.Generation}, endpoints: map[string]*managedEndpoint{"remote": {connection: conn, profile: machine.Profile{Session: "two"}, snapshot: &render.Input{State: state}}}}
	if err := m.selectOyagoi(); err != nil {
		t.Fatal(err)
	}
	for _, item := range conn.queue {
		if item.Type == "workspace_select" && item.Workspace == ws.ID && item.WorkspaceGeneration == ws.Generation {
			return
		}
	}
	t.Fatalf("workspace identity was not sent: %+v", conn.queue)
}

func TestOyagoiProjectionCacheTracksSnapshotAndCatalogReview(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	ws := state.Workspaces[0]
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	e := &managedEndpoint{profile: machine.Profile{Session: "two", Label: "first"}, connection: conn, snapshot: &render.Input{State: state}}
	m := &machineClient{oyagoi: &oyagoiSelection{Machine: "remote", Session: "two", Workspace: ws.ID, Generation: ws.Generation}, endpoints: map[string]*managedEndpoint{"remote": e}}
	first := m.resolveOyagoi()
	if m.resolveOyagoi() != first {
		t.Fatal("unchanged snapshot recomputed")
	}
	e.profile.Label = "renamed"
	second := m.resolveOyagoi()
	if second == first || !strings.Contains(second.label, "renamed") {
		t.Fatal("catalog change not reflected")
	}
	e.connection = nil
	third := m.resolveOyagoi()
	if third == second || !third.sidebar.Offline {
		t.Fatal("disconnect not reflected")
	}
}

func TestOyagoiDeletedWorkspaceDoesNotFollowReusedNumber(t *testing.T) {
	s := model.New(t.TempDir(), 80, 24)
	ws := s.Workspaces[0]
	store := oyagoiStore{filepath.Join(t.TempDir(), "oyagoi.json")}
	selected := oyagoiSelection{Machine: "local", Session: "one", Workspace: ws.ID, Generation: ws.Generation}
	if err := store.set(&selected); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{conn: left, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoiStore: store, oyagoi: &selected, endpoints: map[string]*managedEndpoint{"local": {connection: conn, profile: machine.Profile{ID: "local", Session: "one"}, snapshot: &render.Input{State: s}}}}
	s.Workspaces[0].Generation = "new-generation"
	if err := m.validateOyagoiSnapshot("local"); err != nil {
		t.Fatal(err)
	}
	exists := false
	if err := m.confirmOyagoiSnapshot("local", msg{Workspace: selected.Workspace, WorkspaceGeneration: selected.Generation, OyagoiExists: &exists}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.load(); err != nil || got != nil {
		t.Fatalf("reused ID retained selection: %+v, %v", got, err)
	}
}

func TestOyagoiStaleSnapshotDoesNotClearNewSelection(t *testing.T) {
	state := model.New(t.TempDir(), 80, 24)
	state.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	ws := state.Workspaces[1]
	selection := oyagoiSelection{Machine: "local", Session: "one", Workspace: ws.ID, Generation: ws.Generation}
	store := oyagoiStore{filepath.Join(t.TempDir(), "oyagoi.json")}
	if err := store.set(&selection); err != nil {
		t.Fatal(err)
	}
	stale := model.New(t.TempDir(), 80, 24)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	peer := endpoint.Current()
	conn := &endpointConnection{writer: &writer{conn: left, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m := &machineClient{session: "one", oyagoiStore: store, oyagoi: &selection, endpoints: map[string]*managedEndpoint{"local": {connection: conn, snapshot: &render.Input{State: stale}}}}
	if err := m.validateOyagoiSnapshot("local"); err != nil {
		t.Fatal(err)
	}
	got, err := store.load()
	if err != nil || got == nil || *got != selection {
		t.Fatalf("stale snapshot erased selection: %+v, %v", got, err)
	}
}

func TestOyagoiSidebarUsesSharedSelectionAndConnectionStatus(t *testing.T) {
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "local", Session: "one", Workspace: 5, Generation: "g"}, endpoints: map[string]*managedEndpoint{"local": {}}}
	got := m.oyagoiForSidebar()
	if got == nil || got.Machine != "local" || got.Workspace != 5 || !got.Offline {
		t.Fatalf("offline: %+v", got)
	}
}
