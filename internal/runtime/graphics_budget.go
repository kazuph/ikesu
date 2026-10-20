package runtime

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
)

// API admissions share the supervisor lock so simultaneous streams cannot
// each claim the same remaining session-wide capacity. Clear only frees space.
func (s *Supervisor) SetGraphicsLayer(pid int, frame api.PaneGraphicsSetParams, data []byte, owner uint64) *api.Error {
	image, failure := decodeLayerImage(frame, data)
	if failure != nil {
		return failure
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installGraphicsImage(pid, frame, image, owner)
}

// The caller holds the supervisor lock through admission and installation.
func (s *Supervisor) installGraphicsImage(pid int, frame api.PaneGraphicsSetParams, image kitty.Image, owner uint64) *api.Error {
	if failure := s.admitGraphicsImage(pid, frame, image, owner); failure != nil {
		return failure
	}
	return s.panes[pid].setGraphicsLayer(frame, image, owner)
}

func (s *Supervisor) admitGraphicsImage(pid int, frame api.PaneGraphicsSetParams, image kitty.Image, owner uint64) *api.Error {
	target := s.panes[pid]
	if target == nil {
		return &api.Error{Code: api.ErrPaneNotFound, Message: "graphics pane no longer exists"}
	}
	key := graphicsLayerID(frame.LayerID)
	target.mu.Lock()
	enabled := target.graphicsEnabled
	full := target.graphicsLayers[key] == nil && len(target.graphicsLayers) >= api.MaxGraphicsLayers
	target.mu.Unlock()
	if !enabled {
		return &api.Error{Code: "feature_disabled", Message: "kitty graphics is disabled"}
	}
	if full {
		return &api.Error{Code: "layer_limit", Message: "pane graphics layer limit reached"}
	}
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
	return nil
}
