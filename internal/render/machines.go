package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// Group inputs retain their own workspace and pane IDs. They are composed
// in the client and never merged into a server's session state.
type MachineGroup struct {
	ID, Label, Status, Reason string
	Input                     *Input
}

type MachineHit struct {
	Machine     string
	Workspace   int
	Row         SidebarRow
	Rect        layout.Rect
	Header, New bool
}

type MachineSidebar struct {
	Groups      []MachineGroup
	Active      string
	Scroll      int
	Hits        []MachineHit
	PaneStatus  []string
	DropMachine string
	DropIndex   *int
	DropSection model.Section
}

type machineRow struct {
	group  int
	row    SidebarRow
	header bool
}

func (m *MachineSidebar) draw(f *Frame, active Input) {
	m.Hits = m.Hits[:0]
	if len(m.Groups) == 1 {
		return
	}
	s := active.State
	if !s.SidebarVisible {
		return
	}
	view := SidebarViewForInput(active)
	area := view.Workspace.Rect
	rows := []machineRow{}
	for gi, group := range m.Groups {
		if gi > 0 {
			rows = append(rows, machineRow{group: gi, row: SidebarRow{Kind: RowRule}}, machineRow{group: gi, header: true}, machineRow{group: gi, row: SidebarRow{Kind: RowRule}})
		}
		if group.Input == nil {
			continue
		}
		source := SidebarRows(group.Input.State, group.Input.Panes, group.Input.SidebarConfig)
		skipRule := false
		for _, row := range source[2:] {
			if row.Kind == RowSep {
				break
			}
			if gi > 0 && row.Kind == RowSection && row.Section == model.SectionNone {
				skipRule = true
				continue
			}
			if skipRule && row.Kind == RowRule {
				skipRule = false
				continue
			}
			rows = append(rows, machineRow{group: gi, row: row})
		}
	}
	view.Workspace.Total = len(rows)
	cards, lastCard := 0, 0
	for _, entry := range rows {
		if entry.header || entry.row.Kind == RowSection || entry.row.Kind == RowRule {
			continue
		}
		if entry.row.Kind == RowWorkspace {
			lastCard = 0
		}
		cards++
		lastCard++
	}
	m.Scroll = max(0, min(m.Scroll, min(max(0, len(rows)-area.H), max(0, cards-lastCard))))
	view.Workspace.Offset = m.Scroll
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	width := area.W
	dropBoundary := -1
	dropAtWorkspace := false
	skip, y := m.Scroll, area.Y
	for i := 0; i < len(rows) && y < area.Y+area.H; i++ {
		entry := rows[i]
		heading := entry.header || entry.row.Kind == RowSection || entry.row.Kind == RowRule
		if !heading && skip > 0 {
			skip--
			continue
		}
		if entry.row.Kind == RowRule && i+1 < len(rows) && rows[i+1].header && y+3 > area.Y+area.H {
			break
		}
		if !heading && y == area.Y+area.H-1 {
			y++
			continue
		}
		group := m.Groups[entry.group]
		hit := MachineHit{Machine: group.ID, Row: entry.row, Rect: layout.Rect{X: area.X, Y: y, W: width, H: 1}, Header: entry.header}
		if entry.header {
			style := uv.Style{Fg: f.Palette.Dim, Attrs: uv.AttrBold}
			f.Text(area.X, y, "▾", uv.Style{Fg: f.Palette.Accent}, area.X+width)
			newRect := SectionNewRect(area.X, y, width)
			labelEnd := area.X + width
			if newRect.W > 0 {
				labelEnd = newRect.X
			}
			f.Text(area.X+2, y, truncate(group.Label, max(0, labelEnd-area.X-2)), style, labelEnd)
			if newRect.W > 0 {
				f.Text(newRect.X, y, "[new]", uv.Style{Fg: f.Palette.Text, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}, area.X+width)
				m.Hits = append(m.Hits, MachineHit{Machine: group.ID, Rect: newRect, New: true})
			}
			machineRowBackground(f, area.X, y, width)
		} else if entry.row.Kind == RowRule {
			drawSidebarRow(f, active.State, active, entry.row, area.X, y, width)
			machineRowBackground(f, area.X, y, width)
		} else if group.Input != nil {
			in := *group.Input
			state := *in.State
			in.State = &state
			if group.ID != m.Active {
				state.Active = -1
				in.SelectedWorkspace = nil
				in.Dragging = false
				in.DragSection = ""
			}
			if group.ID == m.DropMachine && m.DropSection != "" {
				in.DragSection = m.DropSection
			}
			drawSidebarRow(f, &state, in, entry.row, area.X, y, width)
			machineRowBackground(f, area.X, y, width)
			if in.SidebarConfig.UI.TransparentSidebarCards {
				clearSidebarCardBackground(f, &state, entry.row, area.X, y, width)
			}
			switch entry.row.Kind {
			case RowWorkspace, RowGit, RowCardBlank:
				hit.Workspace = state.Workspaces[entry.row.Ws].ID
			}
		}
		m.Hits = append(m.Hits, hit)
		var dropIndex *int
		if group.ID == m.Active && group.Input != nil && group.Input.Dragging {
			dropIndex = sectionDropIndex(*group.Input)
		}
		if group.ID == m.DropMachine {
			dropIndex = m.DropIndex
			if dropIndex == nil && m.DropSection != "" && group.Input != nil {
				in := *group.Input
				in.Dragging, in.DragSection = true, m.DropSection
				dropIndex = sectionDropIndex(in)
			}
		}
		if dropIndex != nil {
			drop := *dropIndex
			if entry.row.Kind == RowWorkspace && entry.row.Ws == drop {
				dropBoundary = y
				if group.ID == m.DropMachine && m.DropSection == "" && len(m.Hits) > 1 {
					previous := m.Hits[len(m.Hits)-2]
					if previous.Machine == group.ID && previous.Row.Kind == RowCardBlank && previous.Rect.Y == y-1 {
						dropBoundary = previous.Rect.Y
					}
				}
				dropAtWorkspace = true
			} else if !dropAtWorkspace && (entry.row.Kind == RowWorkspace || entry.row.Kind == RowGit || entry.row.Kind == RowCardBlank) && entry.row.Ws == drop-1 {
				dropBoundary = y + 1
			}
		}
		y++
	}
	if dropBoundary >= area.Y && dropBoundary < area.Y+area.H {
		for x := area.X; x < area.X+width; x++ {
			if cell := f.Cell(x, dropBoundary); cell != nil {
				style := cell.Style
				style.Fg = f.Palette.Accent
				f.Set(x, dropBoundary, "─", style)
			}
		}
	}
	if active.JobsPanel {
		return
	}
	area = view.Agents.Rect
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	y = area.Y
	for _, group := range m.Groups {
		if y >= area.Y+area.H {
			break
		}
		f.Text(area.X, y, "▾ "+group.Label+": "+group.Status, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}, area.X+area.W)
		m.Hits = append(m.Hits, MachineHit{Machine: group.ID, Rect: layout.Rect{X: area.X, Y: y, W: area.W, H: 1}, Header: true})
		y++
		if group.Reason != "" && (group.Status == "awaiting approval" || group.Status == "承認待ち。クリックで再試行") {
			for _, text := range []string{group.Status, group.Reason} {
				x := area.X
				for _, r := range text {
					width := ansi.StringWidth(string(r))
					if x+width > area.X+area.W {
						y++
						x = area.X
					}
					if y >= area.Y+area.H {
						break
					}
					f.Set(x, y, string(r), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
					x += width
				}
				y++
			}
		}
		if group.Input == nil {
			continue
		}
		in := *group.Input
		source := SidebarRows(in.State, in.Panes, in.SidebarConfig)
		agents := false
		for _, row := range source {
			if row.Kind == RowSep {
				agents = true
				continue
			}
			if !agents || y >= area.Y+area.H {
				continue
			}
			drawSidebarRow(f, in.State, in, row, area.X, y, area.W)
			machineRowBackground(f, area.X, y, area.W)
			if in.SidebarConfig.UI.TransparentSidebarCards {
				clearSidebarCardBackground(f, in.State, row, area.X, y, area.W)
			}
			y++
		}
	}
}

func (m *MachineSidebar) drawPaneStatus(f *Frame, in Input, out *Output) {
	if len(m.PaneStatus) == 0 {
		return
	}
	area := in.State.PanesArea()
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text})
	y := area.Y
	for _, line := range m.PaneStatus {
		x := area.X
		for _, r := range line {
			w := ansi.StringWidth(string(r))
			if r == '\n' || x+w > area.X+area.W {
				y++
				x = area.X
				if r == '\n' {
					continue
				}
			}
			f.Set(x, y, string(r), uv.Style{Fg: f.Palette.Text})
			x += w
		}
		y++
	}
	out.AnchorVisible = false
}

func machineRowBackground(f *Frame, x, y, width int) {
	for col := x; col < x+width; col++ {
		if cell := f.Cell(col, y); cell != nil && cell.Style.Bg == nil {
			cell.Style.Bg = f.Palette.PopupBG
		}
	}
}
