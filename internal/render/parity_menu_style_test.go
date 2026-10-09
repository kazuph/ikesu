package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"testing"
)

func TestParityGlobalMenuLabelHighlightAndSeparator(t *testing.T) {
	f := NewFrame(40, 20)
	m := &Menu{Global: true, Items: []MenuItem{{Label: "new workspace"}, {Label: "new tab"}, {Separator: true}, {Label: "restore agents..."}}, Sel: 0, Hover: -1}
	out := &Output{}
	drawMenu(f, m, out)
	selected, unselected, separator := out.MenuItemRows[0], out.MenuItemRows[1], out.MenuItemRows[2]
	if got := f.Cell(selected.X, selected.Y).Style; got.Fg != f.Palette.PopupBG || got.Bg != f.Palette.Accent || got.Attrs != uv.AttrBold {
		t.Fatal(got)
	}
	for _, r := range []struct{ x, y int }{{selected.X + 15, selected.Y}, {unselected.X + 9, unselected.Y}} {
		if got := f.Cell(r.x, r.y).Style; got.Fg != nil || got.Bg != f.Palette.PopupBG || got.Attrs != 0 {
			t.Fatal("padding style", got)
		}
	}
	for x := separator.X; x < separator.X+separator.W; x++ {
		c := f.Cell(x, separator.Y)
		if c.Content != "─" || c.Style.Fg != f.Palette.PanelBG || c.Style.Bg != f.Palette.PopupBG {
			t.Fatal(c)
		}
	}
}
