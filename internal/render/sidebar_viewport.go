package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"math"
)

type SidebarViewport struct {
	Rect          layout.Rect
	Total, Offset int
}

type SidebarPosition struct {
	Row SidebarRow
	Y   int
}

type SidebarView struct {
	Rows              []SidebarPosition
	Workspace, Agents SidebarViewport
}

// The isolated upper-edge captures keep the divider at row 7 at 25, 35 and 45 rows.
const sidebarMinimumDraggedSplit = 7

// Headers and both footers remain fixed; each panel has its own scroll offset.
func SidebarSplitY(s *model.AppState) int {
	area := s.SidebarArea()
	split := .5
	if s.SidebarSectionSplit != nil {
		split = *s.SidebarSectionSplit
	}
	y := int(math.Round(float64(area.H) * split))
	if s.SidebarSectionSplit != nil {
		upper := max(0, area.H-3)
		y = min(upper, max(sidebarMinimumDraggedSplit, y))
	}
	return area.Y + y
}

func SidebarViewFor(s *model.AppState, panes map[int]*PaneSnap, cfg config.Config, workspaceScroll, agentScroll int) SidebarView {
	a := s.SidebarArea()
	iw := max(0, a.W-1)
	mid := SidebarSplitY(s)
	view := SidebarView{
		Workspace: SidebarViewport{Rect: layout.Rect{X: a.X, Y: a.Y + 2, W: iw, H: max(0, mid-a.Y-3)}},
		Agents:    SidebarViewport{Rect: layout.Rect{X: a.X, Y: mid + 2, W: iw, H: max(0, a.Y+a.H-mid-3)}},
	}
	rows := SidebarRows(s, panes, cfg)
	sep := len(rows)
	for i, r := range rows {
		if r.Kind == RowSep {
			sep = i
			break
		}
	}
	view.Workspace.Total = max(0, sep-2)
	view.Agents.Total = max(0, len(rows)-sep-1)
	view.Workspace.Offset = max(0, min(workspaceScroll, max(0, view.Workspace.Total-view.Workspace.Rect.H)))
	view.Agents.Offset = max(0, min(agentScroll, max(0, view.Agents.Total-view.Agents.Rect.H)))
	for i := 0; i < min(2, len(rows)); i++ {
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[i], Y: a.Y + i})
	}
	for n := 0; n < view.Workspace.Rect.H && n+view.Workspace.Offset < view.Workspace.Total; n++ {
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[2+n+view.Workspace.Offset], Y: view.Workspace.Rect.Y + n})
	}
	if sep < len(rows) {
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[sep], Y: mid + 1})
	}
	for n := 0; n < view.Agents.Rect.H && n+view.Agents.Offset < view.Agents.Total; n++ {
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[sep+1+n+view.Agents.Offset], Y: view.Agents.Rect.Y + n})
	}
	return view
}

func SidebarThumb(v SidebarViewport) (height, top int) {
	h := v.Rect.H
	if h <= 0 || v.Total <= h {
		return h, 0
	}
	height = max(1, h*h/v.Total)
	top = v.Offset * (h - height) / (v.Total - h)
	return
}

func drawSidebarScrollbar(f *Frame, v SidebarViewport) {
	if v.Rect.W <= 0 || v.Rect.H <= 0 || v.Total <= v.Rect.H {
		return
	}
	h, top := SidebarThumb(v)
	for y := 0; y < v.Rect.H; y++ {
		glyph, fg := "│", f.Palette.Dim
		if y >= top && y < top+h {
			glyph, fg = "█", f.Palette.Accent
		}
		f.Set(v.Rect.X+v.Rect.W-1, v.Rect.Y+y, glyph, uv.Style{Fg: fg})
	}
}
