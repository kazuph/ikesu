package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
)

func clearSidebarCardBackground(f *Frame, s *model.AppState, row SidebarRow, x, y, width int) {
	switch row.Kind {
	case RowWorkspace, RowGit, RowCardBlank, RowAgent:
	default:
		return
	}
	for col := x; col < x+width; col++ {
		if cell := f.Cell(col, y); cell != nil {
			cell.Style.Bg = nil
		}
	}
	if row.Ws == s.Active && row.Kind != RowAgent {
		if width > 1 {
			f.Set(x, y, "▌", uv.Style{Fg: f.Palette.Accent})
		} else if cell := f.Cell(x, y); cell != nil {
			cell.Style.Bg = f.Palette.PanelBG
		}
	}
}
