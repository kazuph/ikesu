package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// RightAgentRows is the full endpoint card sequence before viewport clipping.
func RightAgentRows(in Input) []SidebarRow {
	cfg := in.SidebarConfig
	if in.ServerUI && len(cfg.UI.Sidebar.Agents.Rows) == 0 {
		cfg.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "workspace"}}, {{Token: "agent"}}}
	}
	rows := SidebarRows(in.State, in.Panes, cfg)
	for i, row := range rows {
		if row.Kind == RowSep {
			return rows[i+1:]
		}
	}
	return nil
}

type machineRightRow struct {
	group  int
	row    SidebarRow
	header bool
	text   string
}

func (m *MachineSidebar) rightRows(active Input) []machineRightRow {
	compact := active.State.RightSidebarArea().W == model.SidebarCollapsedWidth
	var rows []machineRightRow
	if compact {
		for gi, group := range m.Groups {
			if group.Input != nil {
				for _, row := range RightCompactRows(*group.Input) {
					if row.Kind == RowAgent {
						rows = append(rows, machineRightRow{group: gi, row: row})
					}
				}
			}
		}
		rows = append(rows, machineRightRow{row: SidebarRow{Kind: RowSep}})
		for gi, group := range m.Groups {
			if group.Input != nil {
				for i := range group.Input.Jobs {
					rows = append(rows, machineRightRow{group: gi, row: SidebarRow{Kind: RowJob, Line: i}})
				}
			}
		}
		return rows
	}

	for gi, group := range m.Groups {
		if !compact {
			rows = append(rows, machineRightRow{group: gi, header: true, text: "▾ " + group.Label + ": " + group.Status})
			if group.Reason != "" && (group.Status == "awaiting approval" || group.Status == "承認待ち。クリックで承認ページを開く") {
				for _, text := range []string{group.Status, group.Reason} {
					line := ""
					width := 0
					limit := active.State.RightSidebarArea().W - 1
					for _, r := range text {
						w := ansi.StringWidth(string(r))
						if width+w > limit || r == '\n' {
							rows = append(rows, machineRightRow{group: gi, header: true, text: line})
							line = ""
							width = 0
						}
						if r != '\n' {
							line += string(r)
							width += w
						}
					}
					rows = append(rows, machineRightRow{group: gi, header: true, text: line})
				}
			}
		}
		if group.Input == nil {
			continue
		}
		var source []SidebarRow
		if compact {
			source = RightCompactRows(*group.Input)
		} else if active.JobsPanel {
			for i := range group.Input.Jobs {
				source = append(source, SidebarRow{Kind: RowJob, Line: i})
			}
		} else {
			source = RightAgentRows(*group.Input)
		}
		for _, row := range source {
			rows = append(rows, machineRightRow{group: gi, row: row})
		}
	}
	return rows
}
func (m *MachineSidebar) drawRight(f *Frame, active Input) {
	area := active.State.RightSidebarArea()
	if area.W == 0 {
		return
	}
	compact := area.W == model.SidebarCollapsedWidth
	area.X++
	area.W--
	if !compact {
		area.Y = 2
		area.H = max(0, area.H-2)
	}
	rows := m.rightRows(active)
	m.RightScroll = max(0, min(m.RightScroll, max(0, len(rows)-area.H)))
	f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	for n := 0; n < area.H && n+m.RightScroll < len(rows); n++ {
		entry := rows[n+m.RightScroll]
		group := m.Groups[entry.group]
		y := area.Y + n
		hit := MachineHit{Machine: group.ID, Row: entry.row, Header: entry.header, Right: true, Rect: layout.Rect{X: area.X, Y: y, W: area.W, H: 1}}
		m.Hits = append(m.Hits, hit)
		if entry.header {
			f.Text(area.X, y, entry.text, uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}, area.X+area.W)
			continue
		}
		in := *group.Input
		if compact {
			r := entry.row
			g := StatusGlyph{Rune: "─", Color: Dim}
			if r.Kind == RowAgent && in.Panes[r.Pane] != nil {
				g = in.Panes[r.Pane].Status
			}
			if r.Kind == RowJob {
				g.Rune = "●"
				g.Color = in.Jobs[r.Line].Color
			}
			if g.Rune == "" || g.Rune == " " {
				g.Rune = "·"
			}
			style := uv.Style{Fg: f.ThemeColor(g.Color), Bg: f.Palette.PopupBG, Attrs: boldIf(g.Bold)}
			if group.ID == m.Active && r.Kind == RowAgent && in.State.FocusedPane() == r.Pane {
				style.Bg = f.Palette.PanelBG
			}
			f.Set(area.X, y, g.Rune, style)
		} else {
			drawSidebarRow(f, in.State, in, entry.row, area.X, y, area.W)
			machineRowBackground(f, area.X, y, area.W)
			if in.SidebarConfig.UI.TransparentSidebarCards {
				clearSidebarCardBackground(f, in.State, entry.row, area.X, y, area.W)
			}
		}
	}
}
