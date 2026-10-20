package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
)

// Every width uses the expanded rows. Only text loses space; its status cell
// stays at the panel's right edge, including the one-cell interior.
func drawSidebarRow(f *Frame, s *model.AppState, in Input, r SidebarRow, x, y, w int, scrollbar bool) {
	if w <= 0 {
		return
	}
	var glyph StatusGlyph
	switch r.Kind {
	case RowWorkspace:
		glyph = wsGlyph(s, r.Ws, in)
	case RowAgent:
		if r.Line == 0 && in.Panes[r.Pane] != nil {
			glyph = in.Panes[r.Pane].Status
		}
	case RowJob:
		if r.Line >= 0 && r.Line < len(in.Jobs) {
			glyph = StatusGlyph{Rune: "●", Color: in.Jobs[r.Line].Color}
		}
	}
	hasStatus := r.Kind == RowWorkspace || r.Kind == RowAgent && r.Line == 0 || r.Kind == RowJob
	textWidth := w
	if hasStatus {
		textWidth--
	}
	if scrollbar {
		textWidth = min(textWidth, w-2)
	}
	if textWidth > 0 && (w > 1 || hasStatus) {
		drawSidebarRowContent(f, s, in, r, x, y, textWidth)
	}
	if !hasStatus {
		return
	}
	if glyph.Rune == "" || glyph.Rune == " " {
		glyph.Rune = "·"
		if glyph.Color == nil {
			glyph.Color = Dim
		}
	}
	style := uv.Style{Fg: f.ThemeColor(glyph.Color), Attrs: boldIf(glyph.Bold)}
	if cell := f.Cell(x, y); cell != nil {
		style.Bg = cell.Style.Bg
	}
	if w == 1 && r.Kind == RowWorkspace && r.Ws == s.Active {
		style.Bg = f.Palette.PanelBG
	}
	f.Set(x+w-1, y, glyph.Rune, style)
}
