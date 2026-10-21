package runtime

import (
	"bytes"
	"image/png"
	"sort"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/kitty"
)

type graphicsLayer struct {
	placement kitty.Placement
	owner     uint64
}

func graphicsLayerID(id *string) string {
	if id == nil {
		return "primary"
	}
	return *id
}

func (p *Pane) GraphicsEnabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.graphicsEnabled
}

func (p *Pane) setGraphicsLayer(frame api.PaneGraphicsSetParams, image kitty.Image, owner uint64) *api.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.graphicsEnabled {
		return &api.Error{Code: "feature_disabled", Message: "kitty graphics is disabled"}
	}
	key := graphicsLayerID(frame.LayerID)
	old := p.graphicsLayers[key]
	if old != nil && old.owner != 0 && old.owner != owner {
		return &api.Error{Code: "stream_conflict", Message: "graphics layer is owned by another stream"}
	}
	if old == nil && len(p.graphicsLayers) >= api.MaxGraphicsLayers {
		return &api.Error{Code: "layer_limit", Message: "pane graphics layer limit reached"}
	}
	if old != nil {
		image.ID = old.placement.Image.ID
	}
	if p.graphicsLayers == nil {
		p.graphicsLayers = map[string]*graphicsLayer{}
	}
	p.graphicsLayers[key] = &graphicsLayer{owner: owner, placement: kitty.Placement{
		Image: image, X: int(frame.Placement.ViewportCol), Y: int(frame.Placement.ViewportRow),
		Cols: int(frame.Placement.GridCols), Rows: int(frame.Placement.GridRows), Z: int(frame.ZIndex),
	}}
	p.revision.Add(1)
	p.notifyChanged()
	return nil
}

func decodeLayerImage(frame api.PaneGraphicsSetParams, data []byte) (kitty.Image, *api.Error) {
	image := kitty.Image{Width: int(frame.ImageWidth), Height: int(frame.ImageHeight), Data: data}
	invalid := func(message string) (kitty.Image, *api.Error) {
		return kitty.Image{}, &api.Error{Code: "invalid_frame", Message: message}
	}
	switch frame.Format {
	case "rgb":
		image.Format = 24
	case "rgba", "bgra":
		image.Format = 32
	case "png":
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return invalid("invalid PNG image")
		}
		image.Format = 100
		image.Width, image.Height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
	default:
		return invalid("unsupported graphics format")
	}
	if image.Width <= 0 || image.Height <= 0 {
		return invalid("missing image dimensions")
	}
	if image.Format != 100 {
		bytesPerPixel := image.Format / 8
		if len(data)%bytesPerPixel != 0 || uint64(frame.ImageWidth)*uint64(frame.ImageHeight) != uint64(len(data)/bytesPerPixel) {
			return invalid("incorrect pixel data size")
		}
	}
	if frame.Format == "bgra" {
		image.Data = bytes.Clone(data)
		for i := 0; i < len(image.Data); i += 4 {
			image.Data[i], image.Data[i+2] = image.Data[i+2], image.Data[i]
		}
	}
	return image, nil
}

func (p *Pane) ClearGraphicsLayer(id *string, owner uint64) *api.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := graphicsLayerID(id)
	if layer := p.graphicsLayers[key]; layer != nil {
		if layer.owner != 0 && layer.owner != owner {
			return &api.Error{Code: "stream_conflict", Message: "graphics layer is owned by another stream"}
		}
		delete(p.graphicsLayers, key)
		p.revision.Add(1)
		p.notifyChanged()
	}
	return nil
}

// Keep producer layers separate from PTY graphics, whose image IDs belong to
// the application. Viewer snapshots assign collision-free IDs to these layers.
func (p *Pane) appendGraphicsLayers(out []kitty.Placement) []kitty.Placement {
	if len(p.graphicsLayers) == 0 {
		return out
	}
	used := map[uint32]bool{}
	ptyIDs := map[uint32]bool{}
	for _, placement := range out {
		used[placement.Image.ID] = true
		ptyIDs[placement.Image.ID] = true
	}
	keys := make([]string, 0, len(p.graphicsLayers))
	for key, layer := range p.graphicsLayers {
		keys = append(keys, key)
		if layer.placement.Image.ID != 0 {
			used[layer.placement.Image.ID] = true
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		layer := p.graphicsLayers[key]
		id := layer.placement.Image.ID
		if id == 0 || ptyIDs[id] {
			id = ^uint32(0)
			for used[id] {
				id--
			}
			layer.placement.Image.ID = id
		}
		used[id] = true
		placement := layer.placement
		placement.APILayer = true
		if placement.Cols == 0 && p.cellWidth > 0 {
			placement.Cols = (placement.Image.Width + p.cellWidth - 1) / p.cellWidth
		}
		if placement.Rows == 0 && p.cellHeight > 0 {
			placement.Rows = (placement.Image.Height + p.cellHeight - 1) / p.cellHeight
		}
		out = append(out, placement)
	}
	return out
}
