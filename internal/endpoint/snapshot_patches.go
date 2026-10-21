package endpoint

import (
	"fmt"

	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/render"
)

// ResetGraphics keeps text delta state, but forces ordinary complete images.
func (e *SnapshotEncoder) ResetGraphics() {
	e.bases = nil
	e.graphics = nil
}

func (e *SnapshotEncoder) patchGraphics(pane int, original []kitty.Placement, wire *render.PaneSnap, in *render.Input) error {
	if !e.GraphicsPatches {
		return nil
	}
	copied := false
	for index, placement := range original {
		source := placement.Image
		if source.Format != 24 && source.Format != 32 || len(source.Data) < imageCompressionMinimum {
			continue
		}
		key := graphicCacheKey{pane, source.ID}
		previous, exists := e.bases[key]
		if exists {
			patch, err := makeGraphicsPatch(pane, previous, source)
			if err != nil {
				return err
			}
			if patch != nil {
				if !copied {
					wire.Graphics = append([]kitty.Placement(nil), wire.Graphics...)
					copied = true
				}
				wire.Graphics[index].Image.Data = nil
				in.GraphicsPatches = append(in.GraphicsPatches, render.PaneGraphicsPatch{Pane: pane, Placement: index, Patch: *patch})
			}
		}
		canonical, err := canonicalGraphics(source)
		if err != nil {
			return err
		}
		if e.bases == nil {
			e.bases = map[graphicCacheKey]kitty.Image{}
		}
		e.bases[key] = canonical
	}
	return nil
}

func (d *SnapshotDecoder) restoreGraphics(in *render.Input) error {
	d.MissingGraphicsKey = nil
	d.graphicsPayloads = nil
	if d.retainedImages == nil {
		d.retainedImages = map[graphicCacheKey]kitty.Image{}
	}
	textOnly := len(in.GraphicsPatches) == 0
	hasRaster := false
	for pane, snapshot := range in.Panes {
		for index, placement := range snapshot.Graphics {
			if placement.Image.NativeID == 0 {
				hasRaster = true
			}
			if d.graphicsPayloads == nil {
				d.graphicsPayloads = map[[2]int]int{}
			}
			d.graphicsPayloads[[2]int{pane, index}] = len(placement.Image.Data)
			if len(placement.Image.Data) > 0 {
				textOnly = false
			}
		}
	}
	textOnly = textOnly && hasRaster
	for _, item := range in.GraphicsPatches {
		count := 0
		for _, rect := range item.Patch.Rects {
			count += len(rect.PNG)
		}
		if d.graphicsPayloads == nil {
			d.graphicsPayloads = map[[2]int]int{}
		}
		d.graphicsPayloads[[2]int{item.Pane, item.Placement}] = count
	}
	if !d.GraphicsPatches {
		if len(in.GraphicsPatches) > 0 {
			return fmt.Errorf("graphics patch codec was not negotiated")
		}
		return nil
	}
	if d.bases == nil {
		d.bases = map[graphicCacheKey]kitty.Image{}
	}
	alive := map[graphicCacheKey]bool{}
	for pane, snapshot := range in.Panes {
		for index, placement := range snapshot.Graphics {
			img := placement.Image
			key := graphicCacheKey{pane, img.ID}
			alive[key] = true
			if len(img.Data) == 0 {
				if previous, ok := d.retainedImages[key]; ok && img.NativeID == 0 {
					snapshot.Graphics[index].Image = previous
				}
				continue
			}
			d.retainedImages[key] = img
			// Small images cannot be bases for the negotiated large-image codec.
			if img.Format != 100 && len(img.Data) < imageCompressionMinimum {
				delete(d.bases, key)
				continue
			}
			canonical, err := canonicalGraphics(img)
			if err != nil {
				return err
			}
			if len(canonical.Data) >= imageCompressionMinimum {
				d.bases[key] = canonical
				d.retainedImages[key] = canonical
				snapshot.Graphics[index].Image = canonical
			} else {
				delete(d.bases, key)
			}
		}
	}
	for _, item := range in.GraphicsPatches {
		pane := in.Panes[item.Pane]
		if pane == nil || item.Placement < 0 || item.Placement >= len(pane.Graphics) {
			return fmt.Errorf("graphics patch has no placement")
		}
		key := graphicCacheKey{item.Pane, pane.Graphics[item.Placement].Image.ID}
		previous, exists := d.bases[key]
		var restored kitty.Image
		var err error
		if exists {
			restored, err = applyGraphicsPatch(item.Pane, previous, item.Patch)
		}
		if !exists || err != nil {
			if d.MissingGraphicsKey == nil {
				value := item.Patch.Key
				d.MissingGraphicsKey = &value
			}
			// Keep the original patch in Input for the presentation path. An
			// existing complete image remains visible until recovery arrives.
			if exists {
				pane.Graphics[item.Placement].Image = previous
			}
			continue
		}
		d.bases[key] = restored
		d.retainedImages[key] = restored
		pane.Graphics[item.Placement].Image = restored
	}
	if !textOnly {
		for key := range d.retainedImages {
			if !alive[key] {
				delete(d.retainedImages, key)
			}
		}
		for key := range d.bases {
			if !alive[key] {
				delete(d.bases, key)
			}
		}
	}
	return nil
}

// GraphicsPayloadSize counts actual PNG/raw bytes before worker reconstruction,
// excluding metadata and outer transport encoding.
func (d *SnapshotDecoder) GraphicsPayloadSize(pane, placement int) (int, bool) {
	size, exists := d.graphicsPayloads[[2]int{pane, placement}]
	return size, exists
}
