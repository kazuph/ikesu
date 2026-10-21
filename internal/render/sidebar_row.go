package render

import "github.com/kazuph/ikesu/internal/model"

func drawSidebarRow(f *Frame, s *model.AppState, in Input, r SidebarRow, x, y, w int, scrollbar bool) {
	if scrollbar && r.Kind != RowSep {
		w--
	}
	if w > 0 {
		drawSidebarRowContent(f, s, in, r, x, y, w)
	}
}
