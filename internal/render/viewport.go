package render

import "image"

// Viewport keeps the last received surface visible while its producer is
// preparing a new size. It never changes the producer's cells or placements.
func (f *Frame) Viewport(width, height int) *Frame {
	clipped := NewFrame(width, height)
	clipped.Palette = f.Palette
	clipped.GraphicsNamespace = f.GraphicsNamespace
	for y := 0; y < min(f.H, clipped.H); y++ {
		copy(clipped.Cells[y*clipped.W:y*clipped.W+min(f.W, clipped.W)], f.Cells[y*f.W:y*f.W+min(f.W, clipped.W)])
	}
	viewport := image.Rect(0, 0, clipped.W, clipped.H)
	for _, graphic := range f.Graphics {
		p := graphic.Placement
		if p.Virtual {
			clipped.Graphics = append(clipped.Graphics, graphic)
			continue
		}
		bounds := image.Rect(p.X, p.Y, p.X+p.Cols, p.Y+p.Rows)
		visible := bounds.Intersect(viewport)
		if !visible.Empty() {
			if visible != bounds {
				graphic = cropOverlayGraphic(graphic, visible)
			}
			clipped.Graphics = append(clipped.Graphics, graphic)
		}
	}
	for _, placeholder := range f.Placeholders {
		if image.Pt(placeholder.X, placeholder.Y).In(viewport) {
			clipped.Placeholders = append(clipped.Placeholders, placeholder)
		}
	}
	return clipped
}
