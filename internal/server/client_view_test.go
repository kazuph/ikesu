package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"reflect"
	"testing"
)

func TestClientViewListIsPerConnectionAndDisconnectRemovesState(t *testing.T) {
	a, b := &clientConn{id: 11}, &clientConn{id: 12}
	s := &Server{clients: map[*clientConn]bool{a: true, b: true}}
	focused := true
	v := &api.ClientView{MachineID: "mini", Session: "remote", WorkspaceID: "s7", PaneIDs: []string{"p9"}, Focused: &focused}
	s.updateClientView(a, v)
	s.updateClientView(b, &api.ClientView{MachineID: "local", Session: "local", WorkspaceID: "s1", PaneIDs: []string{"p1"}})
	focused = false
	v.PaneIDs[0] = "p999"
	got := s.clientViewList()
	if got.Type != "client_view_list" || len(got.Views) != 2 || !*got.Views[0].Focused || got.Views[0].PaneIDs[0] != "p9" || got.Views[1].Focused != nil {
		t.Fatalf("independent states: %+v", got)
	}
	s.updateClientView(a, &api.ClientView{MachineID: "mini", Session: "remote", WorkspaceID: "s8", PaneIDs: []string{"p10"}})
	if got := s.clientViewList().Views[0]; got.WorkspaceID != "s8" || !reflect.DeepEqual(got.PaneIDs, []string{"p10"}) {
		t.Fatalf("old membership retained: %+v", got)
	}
	delete(s.clients, a)
	if got := s.clientViewList(); len(got.Views) != 1 || got.Views[0].ClientID != 12 {
		t.Fatalf("disconnected client retained: %+v", got)
	}
}
