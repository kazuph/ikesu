package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func rightSidebarRow(r SidebarRow) bool {
	return r.Kind == RowAgent || r.Kind == RowSep || r.Kind == RowJob || r.Kind == RowBlank
}

func RightSidebarJobsTab(s *model.AppState, y int) layout.Rect {
	a := s.RightSidebarArea()
	r := SidebarJobsTab(a.W, y)
	r.X += a.X + 1
	return r
}

// RightCompactRows collapses agent cards to one status cell and appends jobs.
func RightCompactRows(in Input) []SidebarRow {
	var rows []SidebarRow
	seen := map[int]bool{}
	for _, r := range SidebarRows(in.State, in.Panes, in.SidebarConfig) {
		if r.Kind == RowAgent && !seen[r.Pane] {
			r.Line = 0
			rows = append(rows, r)
			seen[r.Pane] = true
		}
	}
	rows = append(rows, SidebarRow{Kind: RowSep})
	for i := range in.Jobs {
		rows = append(rows, SidebarRow{Kind: RowJob, Line: i})
	}
	return rows
}

func drawSidebarCollapsed(f *Frame, s *model.AppState, in Input, area layout.Rect) {
	for y := area.Y; y < area.Y+area.H; y++ {
		f.Set(area.X+area.W-1, y, "│", uv.Style{Fg: f.Palette.Dim})
	}
	v := CollapsedWorkspaceViewport(in)
	for n := 0; n < v.Rect.H && n+v.Offset < len(s.Workspaces); n++ {
		i := n + v.Offset
		g := aggregateGlyph(s.Workspaces[i], in)
		if g.Rune == " " || g.Rune == "" {
			g.Rune = "·"
		}
		style := uv.Style{Fg: f.ThemeColor(g.Color), Attrs: boldIf(g.Bold)}
		if i == s.Active {
			style.Bg = f.Palette.PanelBG
		}
		f.Set(area.X, v.Rect.Y+n, g.Rune, style)
	}
}

func drawRightSidebar(f *Frame, s *model.AppState, in Input) {
	a := s.RightSidebarArea()
	if a.W == 0 || a.H == 0 {
		return
	}
	bg := uv.Style{Fg: f.Palette.Text}
	if in.ServerUI {
		bg.Bg = f.Palette.PopupBG
	}
	f.Fill(a.X, a.Y, a.W, a.H, " ", bg)
	for y := a.Y; y < a.Y+a.H; y++ {
		f.Set(a.X, y, "│", uv.Style{Fg: f.Palette.Dim, Bg: bg.Bg})
	}
	if a.W == model.SidebarCollapsedWidth {
		for _, pos := range CollapsedAgentRows(in) {
			r := pos.Row
			g := StatusGlyph{Rune: "─", Color: Dim}
			if r.Kind == RowAgent {
				if sn := in.Panes[r.Pane]; sn != nil {
					g = sn.Status
				}
			}
			if r.Kind == RowJob {
				g.Rune = "●"
				g.Color = in.Jobs[r.Line].Color
			}
			if g.Rune == "" || g.Rune == " " {
				g.Rune = "·"
			}
			style := uv.Style{Fg: f.ThemeColor(g.Color), Bg: bg.Bg, Attrs: boldIf(g.Bold)}
			if r.Kind == RowAgent && s.FocusedPane() == r.Pane {
				style.Bg = f.Palette.PanelBG
			}
			f.Set(a.X+1, pos.Y, g.Rune, style)
		}
		return
	}
	view := SidebarViewForInput(in)
	for _, pos := range view.Rows {
		if !pos.Right {
			continue
		}
		width := a.W - 1
		if !in.ServerUI && pos.Row.Kind != RowSep && view.Agents.Total > view.Agents.Rect.H {
			width--
		}
		drawSidebarRow(f, s, in, pos.Row, a.X+1, pos.Y, width)
	}
	if !in.ServerUI {
		drawSidebarScrollbar(f, view.Agents, false)
	}
}
