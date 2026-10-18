package render

import (
	"fmt"
	"strings"
	"testing"
)

func TestStage6aShortMenuKeepsSelectedRowVisible(t *testing.T) {
	m := &Menu{X: 59, Y: 3, Hover: -1, Sel: 19}
	for i := 0; i < 20; i++ {
		m.Items = append(m.Items, MenuItem{Label: fmt.Sprintf("item-%02d", i)})
	}
	f := NewFrame(80, 10)
	out := &Output{}
	drawMenu(f, m, out)
	if out.MenuRect.H != 10 || out.MenuRect.Y != 0 {
		t.Fatal("menu outside short screen", out.MenuRect)
	}
	var text strings.Builder
	for y := 0; y < 10; y++ {
		for x := 0; x < 80; x++ {
			text.WriteString(f.Cell(x, y).Content)
		}
	}
	if !strings.Contains(text.String(), "item-19") || strings.Contains(text.String(), "item-00") {
		t.Fatal("selected tail hidden", text.String())
	}
	if out.MenuItemRows[19].Y != 8 || out.MenuItemRows[0].H != 0 {
		t.Fatal("incorrect per-item hit rows", out.MenuItemRows)
	}
}
