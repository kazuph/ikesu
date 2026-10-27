package render

import (
	"image/color"
	"math"
)

var waterBG = color.RGBA{0x0d, 0x2b, 0x3a, 0xff}

// KoiCount derives capacity from a two-row fish plus a one-row separating lane.
func KoiCount(agents, pondW, pondH int) int {
	if agents <= 0 || pondW < 12 || pondH < 3 {
		return 0
	}
	// The original ten-cell fish occupies two rows; one blank row separates lanes.
	return min(agents, pondH/3)
}

func koiHash(i int) float64 {
	x := math.Sin(float64(i)*12.9898+78.233) * 43758.5453
	return x - math.Floor(x)
}

// blankRun returns the start and width of the trailing run of blank cells in
// row y between x0 and x1 (exclusive), leaving one cell of padding.
func blankRun(f *Frame, y, x0, x1 int) (int, int) {
	end := x1
	start := end
	for xx := end - 1; xx >= x0; xx-- {
		c := f.Cell(xx, y)
		if c == nil || (c.Content != " " && c.Content != "") {
			break
		}
		start = xx
	}
	start++
	return start, max(0, end-start)
}
