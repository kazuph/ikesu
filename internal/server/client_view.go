package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"reflect"
	"sort"
)

func (s *Server) updateClientView(c *clientConn, view *api.ClientView) {
	if view == nil {
		return
	}
	copy := *view
	copy.ClientID = c.id
	copy.PaneIDs = append([]string{}, view.PaneIDs...)
	if view.Focused != nil {
		focused := *view.Focused
		copy.Focused = &focused
	}
	s.mu.Lock()
	if !s.clients[c] || reflect.DeepEqual(c.clientView, &copy) {
		s.mu.Unlock()
		return
	}
	c.clientView = &copy
	s.mu.Unlock()
	s.emitEvent("client_view_changed", ev("client_view_changed", "client_id", c.id))
}
func (s *Server) clientViewList() api.ClientViewListResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := api.ClientViewListResult{Type: "client_view_list", Views: []api.ClientView{}}
	for c := range s.clients {
		if c.clientView != nil {
			result.Views = append(result.Views, *c.clientView)
		}
	}
	sort.Slice(result.Views, func(i, j int) bool { return result.Views[i].ClientID < result.Views[j].ClientID })
	return result
}
