package render

import (
	"github.com/kazuph/goterrace/internal/layout"
	"image"
)

// The approved endpoint contract permits at most 32 extra crop placements.
const maxOverlayGraphicsPieces = 32

func graphicsCovers(in Input, out *Output) []image.Rectangle {
	f := out.Frame
	rects := []layout.Rect{out.MenuRect, out.DialogRect, out.HelpRect}
	if in.ServerUI {
		rects = append(rects, out.ToastRect)
	}
	if in.Settings != nil {
		rects = append(rects, SettingsRect(f.W, f.H))
	}
	if in.Navigator != nil {
		rects = append(rects, NavigatorRect(f.W, f.H))
	}
	var covers []image.Rectangle
	for _, r := range rects {
		covers = append(covers, image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H))
	}
	return covers
}

func (f *Frame) coverGraphicsRects(covers []image.Rectangle) {
	var visible, untouched []Graphic
	extra := 0
	for _, graphic := range f.Graphics {
		p := graphic.Placement
		bounds := image.Rect(p.X, p.Y, p.X+p.Cols, p.Y+p.Rows)
		touched := false
		if !p.Virtual {
			for _, cover := range covers {
				touched = touched || !bounds.Intersect(cover).Empty()
			}
		}
		if !touched {
			untouched = append(untouched, graphic)
			visible = append(visible, graphic)
			continue
		}
		pieces := &Frame{Graphics: []Graphic{graphic}}
		for _, cover := range covers {
			pieces.coverGraphics(cover)
		}
		extra += max(0, len(pieces.Graphics)-1)
		visible = append(visible, pieces.Graphics...)
	}
	if extra > maxOverlayGraphicsPieces {
		f.Graphics = untouched
	} else {
		f.Graphics = visible
	}
}

func cropGraphic(g Graphic, visible image.Rectangle) Graphic {
	p := g.Placement
	bounds := image.Rect(p.X, p.Y, p.X+p.Cols, p.Y+p.Rows)
	if visible != bounds {
		sx := (visible.Min.X - bounds.Min.X) * p.Image.Width / bounds.Dx()
		sy := (visible.Min.Y - bounds.Min.Y) * p.Image.Height / bounds.Dy()
		sw := max(1, visible.Dx()*p.Image.Width/bounds.Dx())
		sh := max(1, visible.Dy()*p.Image.Height/bounds.Dy())
		g.Source = image.Rect(sx, sy, sx+sw, sy+sh)
	}
	g.Placement.X, g.Placement.Y = visible.Min.X, visible.Min.Y
	g.Placement.Cols, g.Placement.Rows = visible.Dx(), visible.Dy()
	return g
}

func (f *Frame) coverGraphics(covered image.Rectangle) {
	if covered.Empty() {
		return
	}
	var visible []Graphic
	for _, g := range f.Graphics {
		p := g.Placement
		if p.Virtual {
			visible = append(visible, g)
			continue
		}
		bounds := image.Rect(p.X, p.Y, p.X+p.Cols, p.Y+p.Rows)
		overlap := bounds.Intersect(covered)
		if overlap.Empty() {
			visible = append(visible, g)
			continue
		}
		for _, part := range []image.Rectangle{
			image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, overlap.Min.Y),
			image.Rect(bounds.Min.X, overlap.Max.Y, bounds.Max.X, bounds.Max.Y),
			image.Rect(bounds.Min.X, overlap.Min.Y, overlap.Min.X, overlap.Max.Y),
			image.Rect(overlap.Max.X, overlap.Min.Y, bounds.Max.X, overlap.Max.Y),
		} {
			if !part.Empty() {
				visible = append(visible, cropOverlayGraphic(g, part))
			}
		}
	}
	f.Graphics = visible
}

// UI subtraction splits the existing source rectangle at pixel boundaries;
// the remaining band keeps its final pixel, as observed in the reference.
func cropOverlayGraphic(g Graphic, visible image.Rectangle) Graphic {
	p := g.Placement
	source := g.Source
	if source.Empty() {
		source = image.Rect(0, 0, p.Image.Width, p.Image.Height)
	}
	sx := source.Min.X + (visible.Min.X-p.X)*source.Dx()/p.Cols
	sy := source.Min.Y + (visible.Min.Y-p.Y)*source.Dy()/p.Rows
	ex := source.Min.X + (visible.Max.X-p.X)*source.Dx()/p.Cols
	ey := source.Min.Y + (visible.Max.Y-p.Y)*source.Dy()/p.Rows
	g.Source = image.Rect(sx, sy, max(sx+1, ex), max(sy+1, ey))
	g.Placement.X, g.Placement.Y = visible.Min.X, visible.Min.Y
	g.Placement.Cols, g.Placement.Rows = visible.Dx(), visible.Dy()
	return g
}
