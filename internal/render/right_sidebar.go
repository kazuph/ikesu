package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

type RightSidebarHeader struct {
	Agents, Jobs, Sort layout.Rect
}

func RightSidebarHeaderFor(s *model.AppState, y int) RightSidebarHeader {
	a := s.RightSidebarArea()
	if a.W <= model.SidebarCollapsedWidth {
		return RightSidebarHeader{}
	}
	h := RightSidebarHeader{
		Agents: layout.Rect{X: a.X + 1, Y: y, W: len("[agents]"), H: 1},
		Jobs:   layout.Rect{X: a.X + 1 + len("[agents]"), Y: y, W: len("[jobs]"), H: 1},
		Sort:   layout.Rect{X: a.X + a.W - len(SidebarAgentSortLabel(s)), Y: y, W: len(SidebarAgentSortLabel(s)), H: 1},
	}

	return h
}

func RightSidebarJobsTab(s *model.AppState, y int) layout.Rect {
	return RightSidebarHeaderFor(s, y).Jobs
}

func drawRightSidebarHeader(f *Frame, s *model.AppState, in Input, y int) {
	h := RightSidebarHeaderFor(s, y)
	for _, button := range []struct {
		r        layout.Rect
		label    string
		selected bool
	}{
		{h.Agents, "[agents]", !in.JobsPanel}, {h.Jobs, "[jobs]", in.JobsPanel}, {h.Sort, SidebarAgentSortLabel(s), false},
	} {
		style := uv.Style{Fg: f.Palette.Dim, Attrs: uv.AttrBold}
		if button.selected {
			style.Fg = f.Palette.Accent
		}
		area := s.RightSidebarArea()
		if button.r.X >= area.X+1 && button.r.W > 0 {
			f.Text(button.r.X, button.r.Y, button.label, style, min(area.X+area.W, button.r.X+button.r.W))
		}
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

	view := SidebarViewForInput(in)
	for _, pos := range view.Rows {
		if !pos.Right {
			continue
		}
		width := a.W - 1
		drawSidebarRow(f, s, in, pos.Row, a.X+1, pos.Y, width)
	}
	drawRightSidebarHeader(f, s, in, 0)
	if in.ServerUI {
		for y := a.Y; y < a.Y+a.H; y++ {
			for x := a.X + 1; x < a.X+a.W; x++ {
				if c := f.Cell(x, y); c != nil && c.Style.Bg == nil {
					c.Style.Bg = f.Palette.PopupBG
				}
			}
		}
		if in.SidebarConfig.UI.TransparentSidebarCards {
			for _, pos := range view.Rows {
				if pos.Right {
					clearSidebarCardBackground(f, s, pos.Row, a.X+1, pos.Y, a.W-1)
				}
			}
		}
	}

	if !in.ServerUI {
		drawSidebarScrollbar(f, view.Agents, false)
	}
}
