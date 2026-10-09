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

func SidebarViewFor(s *model.AppState, panes map[int]*PaneSnap, cfg config.Config, workspaceScroll, agentScroll int, serverUI bool) SidebarView {
	a := s.SidebarArea()
	iw := max(0, a.W-1)
	mid := SidebarSplitY(s)
	view := SidebarView{
		Workspace: SidebarViewport{Rect: layout.Rect{X: a.X, Y: a.Y + 2, W: iw, H: max(0, mid-a.Y-3)}},
		Agents:    SidebarViewport{Rect: layout.Rect{X: a.X, Y: mid + 2, W: iw, H: max(0, a.Y+a.H-mid-3)}},
	}
	if serverUI {
		view.Agents.Rect.Y++
		view.Agents.Rect.H = max(0, view.Agents.Rect.H-1)
		if len(cfg.UI.Sidebar.Agents.Rows) == 0 {
			cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}, {{Token: "agent"}}}
		}
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
	if serverUI {
		// Small endpoint captures stop before skipping the final workspace card.
		cards, lastCard := 0, 0
		for _, row := range rows[2:sep] {
			if row.Kind == RowSection || row.Kind == RowRule {
				continue
			}
			if row.Kind == RowWorkspace {
				lastCard = 0
			}
			cards++
			lastCard++
		}
		view.Workspace.Total = min(view.Workspace.Total, view.Workspace.Rect.H+max(0, cards-lastCard))
	}
	view.Agents.Total = max(0, len(rows)-sep-1)
	view.Workspace.Offset = max(0, min(workspaceScroll, max(0, view.Workspace.Total-view.Workspace.Rect.H)))
	view.Agents.Offset = max(0, min(agentScroll, max(0, view.Agents.Total-view.Agents.Rect.H)))
	for i := 0; i < min(2, len(rows)); i++ {
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[i], Y: a.Y + i})
	}
	if serverUI {
		// Endpoint captures scroll card rows while retaining every section heading.
		skip, y := view.Workspace.Offset, view.Workspace.Rect.Y
		for i, row := range rows[2:sep] {
			heading := row.Kind == RowSection || row.Kind == RowRule
			if !heading && skip > 0 {
				skip--
				continue
			}
			if y >= view.Workspace.Rect.Y+view.Workspace.Rect.H {
				break
			}
			// A following section's rule/title/rule group is never clipped halfway.
			if row.Kind == RowRule && i+3 < sep && rows[i+3].Kind == RowSection && y+3 > view.Workspace.Rect.Y+view.Workspace.Rect.H {
				break
			}
			// Rules reach the footer; cards leave its preceding row blank.
			if heading || y < view.Workspace.Rect.Y+view.Workspace.Rect.H-1 {
				view.Rows = append(view.Rows, SidebarPosition{Row: row, Y: y})
			}
			y++
		}
	} else {
		for n := 0; n < view.Workspace.Rect.H && n+view.Workspace.Offset < view.Workspace.Total; n++ {
			view.Rows = append(view.Rows, SidebarPosition{Row: rows[2+n+view.Workspace.Offset], Y: view.Workspace.Rect.Y + n})
		}
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
