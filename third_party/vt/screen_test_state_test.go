package vt

import uv "github.com/charmbracelet/ultraviolet"

// Compare the original RenderBuffer contract (cells and damage), rather than
// backend-private ID numbering or physical row order.
func screenTestState(b *screenBuffer) struct {
	Lines   []uv.Line
	Touched []*uv.LineData
} {
	return struct {
		Lines   []uv.Line
		Touched []*uv.LineData
	}{screenTestLines(b), b.Touched}
}
