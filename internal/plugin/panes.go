package plugin

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
)

func (s *Store) ResolvePane(pluginID, entrypoint string) (Installed, Pane, error) {
	items, err := s.List(pluginID)
	if err != nil {
		return Installed{}, Pane{}, err
	}
	if len(items) != 1 {
		return Installed{}, Pane{}, &api.Error{Code: "plugin_not_found", Message: pluginID}
	}
	owner := items[0]
	if !owner.Enabled {
		return owner, Pane{}, &api.Error{Code: "plugin_disabled", Message: pluginID}
	}
	m, _, err := ReadManifest(owner.ManifestPath, s.Version)
	if err != nil {
		return owner, Pane{}, err
	}
	owner.Manifest = m
	for _, p := range m.Panes {
		if p.ID != entrypoint {
			continue
		}
		if !supported(effectivePlatforms(p.Platforms, m.Platforms)) {
			return owner, p, &api.Error{Code: "platform_unsupported", Message: pluginID + "." + entrypoint}
		}
		return owner, p, nil
	}
	return owner, Pane{}, &api.Error{Code: "plugin_pane_not_found", Message: fmt.Sprintf("%s.%s", pluginID, entrypoint)}
}
