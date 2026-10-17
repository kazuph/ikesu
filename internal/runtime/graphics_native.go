package runtime

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/kitty"
)

func nativeGraphicsImage(frame api.PaneGraphicsSetParams, nativeID uint32) (kitty.Image, *api.Error) {
	w, h := uint64(frame.ImageWidth), uint64(frame.ImageHeight)
	if nativeID == 0 || frame.Format != "rgba" || graphicsLayerID(frame.LayerID) != "primary" || w == 0 || h == 0 || w > api.MaxGraphicsFileDirectBytes/4/h {
		return kitty.Image{}, &api.Error{Code: "invalid_image", Message: "invalid native graphics descriptor"}
	}
	return kitty.Image{NativeID: nativeID, Format: 32, Width: int(w), Height: int(h)}, nil
}

// Prepare the placement without publishing a layer whose pixels have not yet
// reached the terminal. Commit repeats admission after the receipt arrives.
func (s *Supervisor) NativeGraphicsPlacement(pid int, frame api.PaneGraphicsSetParams, nativeID uint32, owner uint64) (kitty.Placement, uint32, *api.Error) {
	image, failure := nativeGraphicsImage(frame, nativeID)
	if failure != nil {
		return kitty.Placement{}, 0, failure
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if failure := s.admitGraphicsImage(pid, frame, image, owner); failure != nil {
		return kitty.Placement{}, 0, failure
	}
	p := s.panes[pid]
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.graphicsEnabled {
		return kitty.Placement{}, 0, &api.Error{Code: "feature_disabled", Message: "kitty graphics is disabled"}
	}
	if p.cellWidth <= 0 || p.cellHeight <= 0 {
		return kitty.Placement{}, 0, &api.Error{Code: "cell_size_unavailable", Message: "exact terminal cell pixels are unavailable"}
	}
	var existing []kitty.Placement
	if p.graphics != nil {
		existing = p.graphics.Visible(p.altScreen)
	}
	existing = p.appendGraphicsLayers(existing)
	replace := uint32(0)
	if old := p.graphicsLayers[graphicsLayerID(frame.LayerID)]; old != nil {
		image.ID, replace = old.placement.Image.ID, old.placement.Image.ID
	} else {
		used := map[uint32]bool{}
		for _, placement := range existing {
			used[placement.Image.ID] = true
		}
		image.ID = ^uint32(0)
		for used[image.ID] {
			image.ID--
		}
	}
	cols, rows := int(frame.Placement.GridCols), int(frame.Placement.GridRows)
	if cols == 0 {
		cols = (image.Width + p.cellWidth - 1) / p.cellWidth
	}
	if rows == 0 {
		rows = (image.Height + p.cellHeight - 1) / p.cellHeight
	}
	return kitty.Placement{Image: image, X: int(frame.Placement.ViewportCol), Y: int(frame.Placement.ViewportRow), Cols: cols, Rows: rows, Z: int(frame.ZIndex)}, replace, nil
}

func (s *Supervisor) SetNativeGraphicsLayer(pid int, frame api.PaneGraphicsSetParams, placement kitty.Placement, owner uint64) *api.Error {
	image, failure := nativeGraphicsImage(frame, placement.Image.NativeID)
	if failure != nil {
		return failure
	}
	if placement.Image.ID == 0 || placement.Image.Format != image.Format || placement.Image.Width != image.Width || placement.Image.Height != image.Height || len(placement.Image.Data) != 0 || placement.Virtual {
		return &api.Error{Code: "invalid_image", Message: "native placement does not match its frame"}
	}
	image.ID = placement.Image.ID
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installGraphicsImage(pid, frame, image, owner)
}
