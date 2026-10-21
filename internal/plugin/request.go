package plugin

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"strings"
)

// RegistryRequest is shared by the offline CLI and online socket dispatcher.
// Commands that execute code or own panes are dispatched by the runtime host.
func (s *Store) RegistryRequest(req *api.Request) (api.Response, bool) {
	var p struct {
		Path     string  `json:"path"`
		PluginID string  `json:"plugin_id"`
		Enabled  *bool   `json:"enabled"`
		Source   *Source `json:"source"`
	}
	switch req.Method {
	case "plugin.link", "plugin.list", "plugin.unlink", "plugin.enable", "plugin.disable":
	default:
		return api.Response{}, false
	}
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error()), true
	}
	fail := func(err error) api.Response { return api.Failure(req.ID, "plugin_error", err.Error()) }
	switch req.Method {
	case "plugin.link":
		enabled := p.Enabled == nil || *p.Enabled
		item, err := s.Link(p.Path, enabled, p.Source)
		if err != nil {
			return fail(err), true
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_linked", "plugin": item}), true
	case "plugin.list":
		items, err := s.List(p.PluginID)
		if err != nil {
			return fail(err), true
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_list", "plugins": items}), true
	case "plugin.unlink":
		if p.PluginID == "" {
			return fail(fmt.Errorf("plugin_id is required")), true
		}
		removed, err := s.Unlink(p.PluginID)
		if err != nil {
			return fail(err), true
		}
		return api.Success(req.ID, map[string]any{"type": "plugin_unlinked", "plugin_id": p.PluginID, "removed": removed}), true
	default:
		item, err := s.SetEnabled(p.PluginID, strings.HasSuffix(req.Method, ".enable"))
		if err != nil {
			return fail(err), true
		}
		return api.Success(req.ID, map[string]any{"type": strings.ReplaceAll(req.Method, ".", "_") + "d", "plugin": item}), true
	}
}
