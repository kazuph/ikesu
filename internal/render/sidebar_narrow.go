package render

import "github.com/kazuph/goterrace/internal/model"

// Narrow panels retain the v0.2.5 expanded layout at width 18. Only its
// visible columns are copied; layout and token budgeting never see the crop.
func sidebarReferenceInput(in Input) Input {
	s := *in.State
	if s.SidebarArea().W > 0 && s.SidebarArea().W < model.SidebarMinDefault {
		s.SidebarVisible, s.SidebarSize = true, model.SidebarMinDefault
	}
	if s.RightSidebarArea().W > 0 && s.RightSidebarArea().W < model.SidebarMinDefault {
		s.RightSidebarMode, s.RightSidebarSize = "", model.SidebarMinDefault
	}
	s.Cols = max(s.Cols, s.ExpandedSidebarWidth()+s.ExpandedRightSidebarWidth()+model.MinimumPaneAreaWidth)
	in.State = &s
	return in
}

func copyNarrowSidebar(dst, src *Frame, actual, reference Input, right bool, rows map[int]SidebarRow) {
	area, ref := actual.State.SidebarArea(), reference.State.SidebarArea()
	if right {
		area, ref = actual.State.RightSidebarArea(), reference.State.RightSidebarArea()
	}
	if area.W == 0 || area.W >= model.SidebarMinDefault {
		return
	}
	x, sx := area.X, ref.X
	if right {
		x++
		sx++
	}
	for y := area.Y; y < area.Y+area.H; y++ {
		start := sx
		if area.W == 2 {
			row, ok := rows[y]
			if ok && sidebarLeadingState(actual, row) {
				// At one content cell, omit only the approved leading blank.
				// Selection rails stay until their treatment is explicitly decided.
				if cell := src.Cell(start, y); cell != nil && cell.Content == " " {
					start++
				}
			}
		}
		for dx := 0; dx < area.W-1; dx++ {
			*dst.Cell(x+dx, y) = *src.Cell(start+dx, y)
		}
		border, refBorder := area.X+area.W-1, ref.X+ref.W-1
		if right {
			border, refBorder = area.X, ref.X
		}
		*dst.Cell(border, y) = *src.Cell(refBorder, y)
	}
}

func drawNarrowSidebar(f *Frame, s *model.AppState, in Input, right bool) {
	reference := sidebarReferenceInput(in)
	ref := NewFrame(reference.State.Cols, f.H)
	ref.Palette = f.Palette
	if right {
		drawRightSidebar(ref, reference.State, reference)
	} else {
		drawSidebar(ref, reference.State, reference)
	}
	rows := map[int]SidebarRow{}
	for _, pos := range SidebarViewForInput(reference).Rows {
		if pos.Right == right {
			rows[pos.Y] = pos.Row
		}
	}
	copyNarrowSidebar(f, ref, in, reference, right, rows)
	if !right && s.SidebarVisible {
		drawSidebarCopyFeedback(f, in)
	}
}

func (m *MachineSidebar) draw(f *Frame, in Input) {
	m.drawExpanded(f, in)
	if in.State.SidebarArea().W >= model.SidebarMinDefault && in.State.RightSidebarArea().W >= model.SidebarMinDefault {
		return
	}
	reference := sidebarReferenceInput(in)
	if reference.State.SidebarArea() == in.State.SidebarArea() && reference.State.RightSidebarArea() == in.State.RightSidebarArea() {
		return
	}
	ref := NewFrame(reference.State.Cols, f.H)
	ref.Palette = f.Palette
	drawSidebar(ref, reference.State, reference)
	drawRightSidebar(ref, reference.State, reference)
	vm := *m
	vm.Hits = nil
	vm.drawExpanded(ref, reference)
	for _, right := range []bool{false, true} {
		rows := map[int]SidebarRow{}
		for _, hit := range vm.Hits {
			if hit.Right == right && !hit.Header && !hit.New {
				rows[hit.Rect.Y] = hit.Row
			}
		}
		copyNarrowSidebar(f, ref, in, reference, right, rows)
	}
}

func sidebarLeadingState(in Input, row SidebarRow) bool {
	return row.Kind == RowWorkspace || row.Kind == RowAgent && row.Line == 0 || row.Kind == RowJob
}
