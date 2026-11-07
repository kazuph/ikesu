package client

import (
	"fmt"
	"path/filepath"
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
	if len(local.queue) != 1 || local.queue[0].Workspace != 0 || len(remote.queue) != 1 || remote.queue[0].Type != "oyagoi_view" || remote.queue[0].Workspace != 4 || remote.queue[0].WorkspaceGeneration != "gen" {
		t.Fatalf("local=%+v remote=%+v", local.queue, remote.queue)
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
	m := &machineClient{session: "one", oyagoiStore: store, oyagoi: &selected, endpoints: map[string]*managedEndpoint{"local": {profile: machine.Profile{ID: "local", Session: "one"}, snapshot: &render.Input{State: s}}}}
	s.Workspaces[0].Generation = "new-generation"
	if err := m.validateOyagoiSnapshot("local"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.load(); err != nil || got != nil {
		t.Fatalf("reused ID retained selection: %+v, %v", got, err)
	}
}

func TestOyagoiSidebarUsesSharedSelectionAndConnectionStatus(t *testing.T) {
	m := &machineClient{session: "one", oyagoi: &oyagoiSelection{Machine: "local", Session: "one", Workspace: 5, Generation: "g"}, endpoints: map[string]*managedEndpoint{"local": {}}}
	got := m.oyagoiForSidebar()
	if got == nil || got.Machine != "local" || got.Workspace != 5 || !got.Offline {
		t.Fatalf("offline: %+v", got)
	}
}
