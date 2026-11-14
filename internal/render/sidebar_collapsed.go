package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

// The divider row is not a workspace row in the connected collapsed strip.
func CollapsedWorkspaceViewport(in Input) SidebarViewport {
	return SidebarViewForInput(in).Workspace
}

func CollapsedAgentRows(in Input) []SidebarPosition {
	var rows []SidebarPosition
	for _, pos := range SidebarViewForInput(in).Rows {
		if pos.Right {
			rows = append(rows, pos)
		}
	}
	return rows
}

func CollapsedAgentViewport(in Input) SidebarViewport {
	return SidebarViewForInput(in).Agents
}

// CollapsedExpandRect is the strip's top cell. A click there restores the
// sidebar; it is empty for any width other than the two-cell strip.
func CollapsedExpandRect(s *model.AppState) layout.Rect {
	area := s.SidebarArea()
	if s.SidebarVisible || area.W != model.SidebarCollapsedWidth || area.H == 0 {
		return layout.Rect{}
	}
	return layout.Rect{X: area.X, Y: area.Y, W: 1, H: 1}
}

func drawCollapsedExpandMark(f *Frame, s *model.AppState, in Input) {
	r := CollapsedExpandRect(s)
	if r.W == 0 {
		return
	}
	style := uv.Style{Fg: f.Palette.Accent, Attrs: uv.AttrBold}
	if in.ServerUI {
		style.Bg = f.Palette.PopupBG
	}
	f.Set(r.X, r.Y, "»", style)
}
