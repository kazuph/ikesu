package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// Group inputs retain their own workspace and pane IDs. They are composed
// in the client and never merged into a server's session state.
type MachineGroup struct {
	ID, Label, Status string
	Input             *Input
}

type MachineHit struct {
	Machine     string
	Workspace   int
	Row         SidebarRow
	Rect        layout.Rect
	Header, New bool
}

type MachineSidebar struct {
	Groups []MachineGroup
	Active string
	Scroll int
	Hits   []MachineHit
}

type machineRow struct {
	group  int
	row    SidebarRow
	header bool
}

func (m *MachineSidebar) draw(f *Frame, active Input) {
	m.Hits = m.Hits[:0]
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
	m.Scroll = max(0, min(m.Scroll, max(0, len(rows)-area.H)))
	view.Workspace.Offset = m.Scroll
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	width := area.W
	if len(rows) > area.H {
		width--
	}
	for i := m.Scroll; i < len(rows) && i-m.Scroll < area.H; i++ {
		entry := rows[i]
		group := m.Groups[entry.group]
		y := area.Y + i - m.Scroll
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
			drawSidebarRow(f, &state, in, entry.row, area.X, y, width)
			if in.SidebarConfig.UI.TransparentSidebarCards {
				clearSidebarCardBackground(f, &state, entry.row, area.X, y, width)
			}
			switch entry.row.Kind {
			case RowWorkspace, RowGit, RowCardBlank:
				hit.Workspace = state.Workspaces[entry.row.Ws].ID
			}
		}
		m.Hits = append(m.Hits, hit)
	}
	drawSidebarScrollbar(f, view.Workspace, s.WorkspaceDensity == "slim" && active.SidebarConfig.UI.Sidebar.Spaces.Rows == nil)
	if active.JobsPanel {
		return
	}
	area = view.Agents.Rect
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	y := area.Y
	for _, group := range m.Groups {
		if y >= area.Y+area.H {
			break
		}
		f.Text(area.X, y, "▾ "+group.Label+": "+group.Status, uv.Style{Fg: f.Palette.Dim}, area.X+area.W)
		m.Hits = append(m.Hits, MachineHit{Machine: group.ID, Rect: layout.Rect{X: area.X, Y: y, W: area.W, H: 1}, Header: true})
		y++
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
			if in.SidebarConfig.UI.TransparentSidebarCards {
				clearSidebarCardBackground(f, in.State, row, area.X, y, area.W)
			}
			y++
		}
	}
}
