package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func drawClientVersion(f *Frame, version string, notice bool, out *Output) {
	if version == "" || notice {
		return
	}
	bar := out.ActionBar
	left, right := bar.Copy.X+bar.Copy.W, bar.Cycle.X
	label := "ikesu v" + version
	width := ansi.StringWidth(label)
	if bar.Area.H == 0 || bar.Copy.W == 0 || bar.Cycle.W == 0 || right-left < width+2 {
		return
	}
	f.Text(left+(right-left-width)/2, bar.Area.Y, label, uv.Style{Fg: f.Palette.Dim}, right-1)
}
