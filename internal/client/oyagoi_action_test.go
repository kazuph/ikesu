package client

import (
	"net"
	"path/filepath"

	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

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
