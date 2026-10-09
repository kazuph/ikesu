package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
)

type JobRow struct {
	ID, Label, Status string
	Color             color.Color
}

func SidebarViewForInput(in Input) SidebarView {
	view := SidebarViewFor(in.State, in.Panes, in.SidebarConfig, in.SidebarScroll, in.AgentScroll)
	if !in.JobsPanel {
		return view
	}
	rows := view.Rows[:0]
	for _, p := range view.Rows {
		if p.Y < view.Agents.Rect.Y {
			rows = append(rows, p)
		}
	}
	view.Rows = rows
	view.Agents.Total = len(in.Jobs)
	view.Agents.Offset = max(0, min(in.JobScroll, max(0, view.Agents.Total-view.Agents.Rect.H)))
	for n := 0; n < view.Agents.Rect.H && n+view.Agents.Offset < view.Agents.Total; n++ {
		view.Rows = append(view.Rows, SidebarPosition{Row: SidebarRow{Kind: RowJob, Line: n + view.Agents.Offset}, Y: view.Agents.Rect.Y + n})
	}
	return view
}

func SidebarJobsTab(sWidth, y int) layout.Rect {
	if sWidth-1 < len(" [agents] [jobs]") {
		return layout.Rect{}
	}
	return layout.Rect{X: len(" [agents] "), Y: y, W: len("[jobs]"), H: 1}
}

func SidebarAgentSortLabel(s *model.AppState) string {
	if s.AgentScope == "sort" {
		return "[priority]"
	}
	return "[grouped]"
}

func SidebarAgentSortRect(s *model.AppState, y int) layout.Rect {
	a := s.SidebarArea()
	w := len(SidebarAgentSortLabel(s))
	return layout.Rect{X: a.X + a.W - 1 - w, Y: y, W: w, H: 1}
}

func drawJobRow(f *Frame, in Input, index, x, y, width int) {
	if index < 0 || index >= len(in.Jobs) {
		return
	}
	j := in.Jobs[index]
	x = f.Text(x, y, "● ", uv.Style{Fg: f.ThemeColor(j.Color)}, x+width)
	f.Text(x, y, truncate(j.Label+" · "+j.Status, max(0, width-2)), uv.Style{Fg: f.Palette.Text}, x+max(0, width-2))
}
