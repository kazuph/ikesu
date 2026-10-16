package runtime

import "github.com/kazuph/goterrace/internal/api"

// API admissions share the supervisor lock so simultaneous streams cannot
// each claim the same remaining session-wide capacity. Clear only frees space.
func (s *Supervisor) SetGraphicsLayer(pid int, frame api.PaneGraphicsSetParams, data []byte, owner uint64) *api.Error {
	image, failure := decodeLayerImage(frame, data)
	if failure != nil {
		return failure
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.panes[pid]
	if target == nil {
		return &api.Error{Code: api.ErrPaneNotFound, Message: "graphics pane no longer exists"}
	}
	key := graphicsLayerID(frame.LayerID)
	var bytes uint64
	layers := 0
	for _, pane := range s.panes {
		pane.mu.Lock()
		for id, layer := range pane.graphicsLayers {
			if pane == target && id == key {
				if layer.owner != 0 && layer.owner != owner {
					pane.mu.Unlock()
					return &api.Error{Code: "stream_conflict", Message: "graphics layer is owned by another stream"}
				}
				continue
			}
			layers++
			bytes += uint64(len(layer.placement.Image.Data))
		}
		pane.mu.Unlock()
	}
	if layers >= api.MaxGraphicsTotalLayers {
		return &api.Error{Code: "layer_limit", Message: "session graphics layer limit reached"}
	}
	if bytes > api.MaxGraphicsInlineBytes || uint64(len(image.Data)) > api.MaxGraphicsInlineBytes-bytes {
		return &api.Error{Code: "graphics_budget_exceeded", Message: "session inline graphics budget exceeded"}
	}
	return target.setGraphicsLayer(frame, image, owner)
}
