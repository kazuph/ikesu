package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"math"
)

func drawGutter(f *Frame, inner layout.Rect, snap *PaneSnap, off int, focused bool) {
	if snap == nil || snap.ScrollN <= 0 || inner.H <= 0 || snap.SuppressScrollbar {
		return
	}
	trackColor, thumbColor, thumb := f.Palette.SurfDim, f.Palette.Dim, "▕"
	if focused {
		trackColor, thumbColor, thumb = f.Palette.Dim, f.Palette.Overlay1, "▐"
	}
	gx := inner.X + inner.W - 1
	for y := inner.Y; y < inner.Y+inner.H; y++ {
		f.Set(gx, y, "▕", uv.Style{Fg: trackColor})
	}
	if snap == nil || snap.ScrollN <= 0 || inner.H <= 0 {
		return
	}
	total := snap.ScrollN + inner.H
	th := int(math.Round(float64(inner.H*inner.H) / float64(total)))
	if th < 1 {
		th = 1
	}
	if th > inner.H {
		th = inner.H
	}
	// Thumb top: bottom-aligned at offset 0, top-aligned at max offset.
	track := inner.H - th
	top := inner.Y + track
	if snap.ScrollN > 0 {
		frac := float64(off) / float64(snap.ScrollN)
		top = inner.Y + int(math.Round((1-frac)*float64(track)))
	}
	for y := top; y < top+th && y < inner.Y+inner.H; y++ {
		f.Set(gx, y, thumb, uv.Style{Fg: thumbColor})
	}
}
