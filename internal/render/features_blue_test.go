package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
	"testing"
)

func TestCustomBlueAppliesOnlyToFinishedToastIndicator(t *testing.T) {
	in := Input{State: model.New("/tmp", 120, 35), Toast: &Toast{Title: "1 background", Kind: "finished"}}
	in.SidebarConfig.Theme.Custom = map[string]string{"blue": "#010203"}
	out := Compose(in)
	r := out.ToastRect
	if got := out.Frame.Cell(r.X+1, r.Y+1).Style.Fg; got != (color.RGBA{1, 2, 3, 255}) {
		t.Fatal(got)
	}
	if out.Frame.Palette.Accent == (color.RGBA{1, 2, 3, 255}) {
		t.Fatal("blue changed accent")
	}
	in.Toast.Kind = "needs_attention"
	out = Compose(in)
	if got := out.Frame.Cell(r.X+1, r.Y+1).Style.Fg; got != out.Frame.Palette.Red {
		t.Fatal(got)
	}
}
