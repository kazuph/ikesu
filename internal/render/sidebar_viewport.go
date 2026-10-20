package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

type SidebarViewport struct {
	Rect          layout.Rect
	Total, Offset int
}

type SidebarPosition struct {
	Right bool
	Row   SidebarRow
	Y     int
}

type SidebarView struct {
	Rows              []SidebarPosition
	Workspace, Agents SidebarViewport
}

// SidebarSplitY is retained as the workspace footer anchor; there is no panel split.
func SidebarSplitY(s *model.AppState) int {
	a := s.SidebarArea()
	return a.Y + max(0, a.H-2)
}

func SidebarViewFor(s *model.AppState, panes map[int]*PaneSnap, cfg config.Config, workspaceScroll, agentScroll int, serverUI bool, selected *int) SidebarView {
	a := s.SidebarArea()
	iw := max(0, a.W-1)
	mid := SidebarSplitY(s)
	right := s.RightSidebarArea()
	view := SidebarView{
		Workspace: SidebarViewport{Rect: layout.Rect{X: a.X, Y: a.Y + 2, W: iw, H: max(0, mid-a.Y-3)}},
		Agents:    SidebarViewport{Rect: layout.Rect{X: right.X + 1, Y: 2, W: max(0, right.W-1), H: max(0, right.H-2)}},
	}
	if serverUI {
		if len(cfg.UI.Sidebar.Agents.Rows) == 0 {
			cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}, {{Token: "agent"}}}
		}
	}
	rows := sidebarRows(s, panes, cfg, selected)
	stickyCards := serverUI || (s.WorkspaceDensity == "slim" && cfg.UI.Sidebar.Spaces.Rows == nil)
	sep := len(rows)
	for i, r := range rows {
		if r.Kind == RowSep {
			sep = i
			break
		}
	}
	view.Workspace.Total = max(0, sep-2)
	clippedFirstCard := false
	if !serverUI {
		for i := 2; i < sep; i++ {
			if rows[i].Kind != RowWorkspace {
				continue
			}
			end := i + 1
			for end < sep && rows[end].Ws == rows[i].Ws && (rows[end].Kind == RowGit || rows[end].Kind == RowCardBlank) {
				end++
			}
			clippedFirstCard = end-2 > view.Workspace.Rect.H
			if clippedFirstCard {
				view.Workspace.Total = min(view.Workspace.Total, view.Workspace.Rect.H)
			}
			break
		}
	}
	if stickyCards {
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
		if s.WorkspaceDensity == "slim" && cfg.UI.Sidebar.Spaces.Rows == nil && cfg.UI.Sidebar.Spaces.RowGap == 0 && lastCard == 2 {
			// The observed slim endpoint stops after a complete name/git card.
			overflow := max(0, view.Workspace.Total-view.Workspace.Rect.H)
			overflow = (overflow + lastCard - 1) / lastCard * lastCard
			view.Workspace.Total = view.Workspace.Rect.H + min(overflow, max(0, cards-lastCard))
		}
	}
	view.Agents.Total = max(0, len(rows)-sep-1)
	if view.Workspace.Rect.H < 2 {
		view.Workspace.Total = 0
	}
	view.Workspace.Offset = max(0, min(workspaceScroll, max(0, view.Workspace.Total-view.Workspace.Rect.H)))
	view.Agents.Offset = max(0, min(agentScroll, max(0, view.Agents.Total-view.Agents.Rect.H)))
	for i := 0; i < min(2, len(rows)); i++ {
		if serverUI && i == 1 && view.Workspace.Rect.H < 2 {
			continue
		}
		view.Rows = append(view.Rows, SidebarPosition{Row: rows[i], Y: a.Y + i})
	}
	if stickyCards && view.Workspace.Rect.H >= 2 {
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
			if !serverUI && !clippedFirstCard || heading || y < view.Workspace.Rect.Y+view.Workspace.Rect.H-1 {
				view.Rows = append(view.Rows, SidebarPosition{Row: row, Y: y})
			}
			y++
		}
	} else if !serverUI {
		for n := 0; n < view.Workspace.Rect.H && n+view.Workspace.Offset < view.Workspace.Total; n++ {
			index := 2 + n + view.Workspace.Offset
			row := rows[index]
			if n == view.Workspace.Rect.H-1 && (row.Kind == RowCardBlank || row.Kind == RowGit && index+1 < sep && rows[index+1].Kind == RowCardBlank) {
				continue
			}
			if clippedFirstCard && n == view.Workspace.Rect.H-1 && (row.Kind == RowWorkspace || row.Kind == RowGit || row.Kind == RowCardBlank) {
				continue
			}
			view.Rows = append(view.Rows, SidebarPosition{Row: row, Y: view.Workspace.Rect.Y + n})
		}
	}
	if sep < len(rows) {
		view.Rows = append(view.Rows, SidebarPosition{Right: true, Row: rows[sep], Y: 0})
	}
	for n := 0; n < view.Agents.Rect.H && n+view.Agents.Offset < view.Agents.Total; n++ {
		view.Rows = append(view.Rows, SidebarPosition{Right: true, Row: rows[sep+1+n+view.Agents.Offset], Y: view.Agents.Rect.Y + n})
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

func drawSidebarScrollbar(f *Frame, v SidebarViewport, slim bool) {
	if v.Rect.W <= 1 || v.Rect.H <= 0 || v.Total <= v.Rect.H {
		return
	}
	h, top := SidebarThumb(v)
	for y := 0; y < v.Rect.H; y++ {
		glyph, fg := "│", f.Palette.Dim
		if y >= top && y < top+h {
			glyph, fg = "█", f.Palette.Accent
		}
		if slim {
			glyph, fg = "▕", f.Palette.PanelBG
			if y >= top && y < top+h {
				fg = f.Palette.Dim
			}
		}
		f.Set(v.Rect.X+v.Rect.W-1, v.Rect.Y+y, glyph, uv.Style{Fg: fg})
	}
}
