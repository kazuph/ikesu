package client

import (
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func BenchmarkOyagoiManyMachinesAndWorkspaces(b *testing.B) {
	const machines, workspaces = 16, 1000
	state := &model.AppState{}
	for i := 0; i < workspaces; i++ {
		state.Workspaces = append(state.Workspaces, &model.Workspace{ID: i + 1, Generation: fmt.Sprint(i + 1), Name: fmt.Sprint(i + 1), CustomName: true})
	}
	peer := endpoint.Current()
	m := &machineClient{oyagoi: &oyagoiSelection{Machine: "m0", Session: "session", Workspace: workspaces, Generation: fmt.Sprint(workspaces)}, endpoints: map[string]*managedEndpoint{}}
	for i := 0; i < machines; i++ {
		id := fmt.Sprintf("m%d", i)
		m.endpoints[id] = &managedEndpoint{profile: machine.Profile{ID: id, Label: id, Session: "session", Enabled: true}, connection: &endpointConnection{writer: &writer{available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}}
	}
	m.endpoints["m0"].snapshot = &render.Input{State: state}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.oyagoiForSidebar()
		for id := range m.endpoints {
			if err := m.syncOyagoi(id); err != nil {
				b.Fatal(err)
			}
		}
	}
}
