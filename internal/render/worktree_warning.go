package render

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

func drawEndpointWorktreeWarning(f *Frame, in Input, out *Output) {
	bar := out.ActionBar
	if bar.Area.H == 0 || bar.Copy.W == 0 || bar.Cycle.W == 0 {
		return
	}
	// The endpoint captures leave two cells after the COPY button.
	x := bar.Copy.X + bar.Copy.W + 2
	w := max(0, bar.Cycle.X-x-2)
	style := uv.Style{Fg: f.Palette.Red}
	if in.Dialog != nil || in.Help != nil || in.Settings != nil {
		style.Attrs |= uv.AttrFaint
	}
	label := truncate(strings.TrimSuffix(in.ConfigWarning, "."), w)
	f.Text(x, bar.Area.Y, label, style, x+w)
}
