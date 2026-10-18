package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"path/filepath"
	"sort"
	"strings"
)

// --- sidebar ---

type SidebarRowKind int

const (
	RowWorkspace SidebarRowKind = iota
	RowAgent
	RowSep
	RowTitle
	RowSection
	RowRule
	RowBlank
	RowGit
	RowCardBlank
	RowJob
	RowMobileSpaces
	RowMobileNewWorkspace
	RowMobileTabs
	RowMobileNewTab
	RowMobileTab
	RowMobileMenu
	RowMobileMenuItem
)

type SidebarRow struct {
	Kind     SidebarRowKind
	Ws, Pane int
	Section  model.Section
	Line     int
}

func SidebarRows(s *model.AppState, panes map[int]*PaneSnap, configs ...config.Config) []SidebarRow {
	var cfg config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	return sidebarRows(s, panes, cfg, nil)
}

func sidebarRows(s *model.AppState, panes map[int]*PaneSnap, cfg config.Config, selected *int) []SidebarRow {
	rows := []SidebarRow{{Kind: RowTitle}, {Kind: RowRule}}
	for _, sec := range model.SectionOrder {
		var cards []SidebarRow
		count := 0
		for wi, w := range s.Workspaces {
			if w.SidebarSection() != sec || (s.WorktreeHidden(wi) && (selected == nil || *selected != wi)) {
				continue
			}
			count++
			if !s.CollapsedSections[sec] {
				cards = append(cards, SidebarRow{Kind: RowWorkspace, Ws: wi})
				if w.Worktree == nil || !w.Worktree.IsLinkedWorktree {
					count := 2
					if cfg.UI.Sidebar.Spaces.Rows != nil {
						count = len(cfg.UI.Sidebar.Spaces.Rows)
					}
					for line := 1; line < count; line++ {
						cards = append(cards, SidebarRow{Kind: RowGit, Ws: wi, Line: line})
					}
					if s.WorkspaceDensity != "slim" && cfg.UI.Sidebar.Spaces.Rows == nil {
						cards = append(cards, SidebarRow{Kind: RowCardBlank, Ws: wi})
					}
					for gap := 0; gap < cfg.UI.Sidebar.Spaces.RowGap; gap++ {
						cards = append(cards, SidebarRow{Kind: RowBlank})
					}
				}
			}
		}
		if count == 0 {
			continue
		}
		if len(rows) > 2 {
			rows = append(rows, SidebarRow{Kind: RowRule})
		}
		rows = append(rows, SidebarRow{Kind: RowSection, Section: sec}, SidebarRow{Kind: RowRule, Section: sec})
		rows = append(rows, cards...)
	}
	var agents []SidebarRow
	for wi, w := range s.Workspaces {
		if s.CollapsedSections[w.SidebarSection()] {
			continue
		}
		for _, t := range w.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if sn := panes[pid]; sn != nil && sn.Agent != "" {
					agents = append(agents, SidebarRow{Kind: RowAgent, Ws: wi, Pane: pid})
				}
			}
		}
	}
	if s.AgentScope == "sort" {
		sort.SliceStable(agents, func(i, j int) bool {
			left, right := panes[agents[i].Pane], panes[agents[j].Pane]
			if rank := agentRank(left.Status.Key); rank != agentRank(right.Status.Key) {
				return rank < agentRank(right.Status.Key)
			}
			if cfg.UI.AgentPanelSort == "priority" {
				return left.StateChangeSeq > right.StateChangeSeq
			}
			return false
		})
	}
	agents = projectAgentRows(s, panes, agents)
	rows = append(rows, SidebarRow{Kind: RowSep})
	for _, r := range agents {
		panel := cfg.UI.Sidebar.Agents
		configured := panel.Rows
		if specific, ok := panel.RowsByAgent[panes[r.Pane].Agent]; ok {
			configured = specific
		}
		custom := hasConfiguredAgentRows(cfg, panes[r.Pane].Agent)
		if custom {
			configured = visibleAgentRows(s, panes, r, configured)
		}
		count := max(1, len(configured))
		if custom {
			count = len(configured)
		}
		for line := 0; line < count; line++ {
			r.Line = line
			rows = append(rows, r)
		}
		for i := 0; i < panel.RowGap; i++ {
			rows = append(rows, SidebarRow{Kind: RowBlank})
		}
	}
	return rows
}
func agentRank(key string) int {
	switch key {
	case "blocked":
		return 0
	case "working":
		return 1
	case "done":
		return 2
	case "idle":
		return 3
	}
	return 4
}

// SidebarContent returns the interior rect of the (expanded) sidebar box and
// how many rows are visible. Rows scroll inside it; the last interior row is
// the fixed [menu]/[new] footer.
func SidebarContent(area layout.Rect) (x, y, w, rows int) {
	x = area.X
	y = area.Y
	w = area.W - 1
	rows = area.H - 2 // minus top border, footer row, bottom border
	if rows < 0 {
		rows = 0
	}
	return
}

// SidebarFooter returns the rect of the fixed footer row inside the box.
func SidebarFooter(s *model.AppState) layout.Rect {
	area := s.SidebarArea()
	return layout.Rect{X: area.X, Y: SidebarSplitY(s) - 1, W: max(0, area.W-1), H: 1}
}

// SidebarToggleRect returns the cell of the « / » collapse-expand button.
func SidebarToggleRect(s *model.AppState) layout.Rect {
	area := s.SidebarArea()
	if s.SidebarVisible {
		return layout.Rect{X: area.X + area.W - 2, Y: area.Y + area.H - 1, W: 1, H: 1}
	}
	return layout.Rect{X: area.X + (area.W-1)/2, Y: area.Y + area.H - 1, W: 1, H: 1}
}

// SidebarMenuRect / SidebarNewRect locate the footer's [menu] and [new].
func SidebarMenuRect(s *model.AppState) layout.Rect {
	fr := SidebarFooter(s)
	return layout.Rect{X: fr.X, Y: fr.Y, W: len("[menu]"), H: 1}
}
func SidebarNewRect(s *model.AppState) layout.Rect {
	fr := SidebarFooter(s)
	return layout.Rect{X: fr.X + fr.W - len("[new]"), Y: fr.Y, W: len("[new]"), H: 1}
}

func drawSidebar(f *Frame, s *model.AppState, in Input) {
	area := s.SidebarArea()
	if area.W == 0 || area.H == 0 {
		return
	}
	if in.ServerUI {
		f.Fill(area.X, area.Y, area.W, area.H, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG})
	}
	if !s.SidebarVisible {
		drawSidebarCollapsed(f, s, in, area)
		return
	}
	border := uv.Style{Fg: f.Palette.Dim}
	if in.ServerUI {
		border.Bg = f.Palette.PopupBG
	}
	boundary := border
	if in.SelectedWorkspace != nil {
		boundary.Fg = f.Palette.Accent
	}
	for y := area.Y; y < area.Y+area.H; y++ {
		f.Set(area.X+area.W-1, y, "│", boundary)
	}
	ix, iy, iw, _ := SidebarContent(area)
	if iw < 1 {
		return
	}

	view := SidebarViewForInput(in)
	for _, pos := range view.Rows {
		if pos.Right {
			continue
		}
		width := iw
		if !in.ServerUI && pos.Y >= view.Workspace.Rect.Y && pos.Y < view.Workspace.Rect.Y+view.Workspace.Rect.H && view.Workspace.Total > view.Workspace.Rect.H {
			width--
		}
		drawSidebarRow(f, s, in, pos.Row, ix, pos.Y, width)
	}
	if !in.ServerUI {
		drawSidebarScrollbar(f, view.Workspace, s.WorkspaceDensity == "slim" && in.SidebarConfig.UI.Sidebar.Spaces.Rows == nil)
	}
	drop := in.DropIndex
	if drop == nil && in.Dragging {
		drop = sectionDropIndex(in)
	}
	if drop != nil && in.Dragging {
		boundary := -1
		for _, pos := range view.Rows {
			if pos.Row.Kind == RowWorkspace && pos.Row.Ws == *drop {
				boundary = pos.Y
				break
			}
			if (pos.Row.Kind == RowWorkspace || pos.Row.Kind == RowGit || pos.Row.Kind == RowCardBlank) && pos.Row.Ws == *drop-1 {
				boundary = pos.Y + 1
			}
		}
		if boundary >= view.Workspace.Rect.Y && boundary < view.Workspace.Rect.Y+view.Workspace.Rect.H {
			for x := ix; x < ix+iw; x++ {
				if cell := f.Cell(x, boundary); cell != nil {
					style := cell.Style
					style.Fg = f.Palette.Accent
					f.Set(x, boundary, "─", style)
				}
			}
		}
	}

	fr := SidebarFooter(s)
	if fr.W > 0 && fr.Y >= iy {
		f.Text(fr.X, fr.Y, "[menu]", uv.Style{Fg: f.Palette.Dim}, fr.X+fr.W)
		f.Text(fr.X+fr.W-len("[new]"), fr.Y, "[new]", uv.Style{Fg: f.Palette.Dim}, fr.X+fr.W)
	}
	wr := SidebarWidthRect(s)
	if wr.W > 0 {
		f.Text(wr.X, wr.Y, s.PresetLabel(), uv.Style{Fg: f.Palette.Accent, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}, wr.X+wr.W)
	}
	if !in.ServerUI {
		f.Set(area.X+area.W-2, area.Y+area.H-1, "«", uv.Style{Fg: f.Palette.Dim})
	}
	if in.ServerUI {
		for y := area.Y; y < area.Y+area.H; y++ {
			for x := area.X; x < area.X+area.W; x++ {
				if c := f.Cell(x, y); c != nil && c.Style.Bg == nil {
					c.Style.Bg = f.Palette.PopupBG
				}
			}
		}
	}
	if in.ServerUI && in.SidebarConfig.UI.TransparentSidebarCards {
		for _, pos := range view.Rows {
			if pos.Right {
				continue
			}
			clearSidebarCardBackground(f, s, pos.Row, ix, pos.Y, iw)
		}
	}
	if in.Copied != "" && !in.ServerUI && area.W > 1 && area.H > 0 {
		x, y, w := area.X, area.Y+area.H-1, area.W-1
		f.Fill(x, y, w-1, 1, " ", uv.Style{})
		copyStyle := uv.Style{Fg: f.Palette.Green, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}
		label := truncate(in.Copied, w-1)
		f.Text(x, y, label, copyStyle, x+w-1)
		prefix := strings.TrimSuffix(in.Copied, " lines")
		for xx := x; xx < x+min(len([]rune(prefix)), len([]rune(label))); xx++ {
			if cell := f.Cell(xx, y); cell != nil {
				cell.Style.Underline = uv.UnderlineSingle
			}
		}
	}
}

func sectionDropIndex(in Input) *int {
	if in.DropIndex != nil || !in.ServerUI || !in.Dragging || in.DragSection == "" {
		return in.DropIndex
	}
	for _, row := range SidebarRows(in.State, in.Panes, in.SidebarConfig) {
		if row.Kind == RowWorkspace && in.State.Workspaces[row.Ws].SidebarSection() == in.DragSection {
			index := row.Ws
			return &index
		}
	}
	return nil
}

func SidebarWidthRect(s *model.AppState) layout.Rect {
	a := s.SidebarArea()
	if !s.SidebarVisible || a.W-1 < 8 || a.H < 1 {
		return layout.Rect{}
	}
	return layout.Rect{X: a.X, Y: a.Y + a.H - 1, W: 8, H: 1}
}
func SectionNewRect(x, y, w int) layout.Rect {
	if w < 6 {
		return layout.Rect{}
	}
	return layout.Rect{X: x + w - 5, Y: y, W: 5, H: 1}
}
func drawSidebarRow(f *Frame, s *model.AppState, in Input, r SidebarRow, ix, y, iw int) {
	border := uv.Style{Fg: f.Palette.Dim}
	switch r.Kind {
	case RowTitle:
		if in.ServerUI {
			f.Text(ix, y, " spaces", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}, ix+iw)
			return
		}
		border.Attrs = uv.AttrBold
		f.Text(ix, y, " spaces", border, ix+iw)
		v := s.WorkspaceDensity
		if v != "slim" {
			v = "full"
		}
		label := "[" + v + "]"
		f.Text(ix+iw-len(label), y, label, border, ix+iw)
	case RowRule:
		for x := ix; x < ix+iw; x++ {
			f.Set(x, y, "─", border)
		}
	case RowSection:
		style := uv.Style{Fg: f.Palette.Dim, Attrs: uv.AttrBold}
		arrow := "▾"
		if s.CollapsedSections[r.Section] {
			arrow = "▸"
		}
		if in.DragSection == r.Section {
			style = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}
			f.Fill(ix, y, iw, 1, " ", style)
		}
		limit := iw
		if iw >= 6 {
			limit -= 6
		}
		if in.DragSection == r.Section {
			f.Text(ix, y, truncate(arrow+" "+r.Section.Label(), limit), style, ix+limit)
		} else {
			f.Text(ix, y, arrow, uv.Style{Fg: f.Palette.Accent}, ix+limit)
			f.Text(ix+2, y, truncate(r.Section.Label(), max(0, limit-2)), style, ix+limit)
		}
		nr := SectionNewRect(ix, y, iw)
		if nr.W > 0 {
			f.Text(nr.X, y, "[new]", uv.Style{Fg: f.Palette.Text, Bg: style.Bg, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}, ix+iw)
		}
	case RowSep:
		agentStyle := border
		if !in.JobsPanel {
			agentStyle.Fg = f.Palette.Accent
			agentStyle.Attrs = uv.AttrBold
		}
		f.Text(ix+1, y, "[agents]", agentStyle, ix+iw)
		jr := RightSidebarJobsTab(s, y)
		if jr.W > 0 {
			jobStyle := border
			if in.JobsPanel {
				jobStyle.Fg = f.Palette.Accent
				jobStyle.Attrs = uv.AttrBold
			}
			f.Text(jr.X, y, "[jobs]", jobStyle, jr.X+jr.W)
		}
		if !in.JobsPanel {
			sortStyle := border
			sortStyle.Attrs = uv.AttrBold
			r := SidebarAgentSortRect(s, y)
			f.Text(r.X, y, SidebarAgentSortLabel(s), sortStyle, ix+iw)
		}
	case RowJob:
		drawJobRow(f, in, r.Line, ix, y, iw)
	case RowWorkspace, RowGit, RowCardBlank:
		if rows := in.SidebarConfig.UI.Sidebar.Spaces.Rows; len(rows) > 0 {
			line := 0
			if r.Kind == RowGit {
				line = r.Line
			}
			if line < len(rows) {
				drawConfiguredSpace(f, s, in, r, rows[line], ix, y, iw)
			}
			return
		}
		ws := s.Workspaces[r.Ws]
		active := r.Ws == s.Active
		dragged := in.Dragging && r.Ws == in.DraggedWorkspace
		sty := uv.Style{Fg: f.Palette.Subtle}
		selected := in.SelectedWorkspace != nil && *in.SelectedWorkspace == r.Ws
		if in.ServerUI && !active && !dragged && !selected {
			sty.Bg = f.Palette.Surface
			f.Fill(ix, y, iw, 1, " ", uv.Style{Fg: f.Palette.Text, Bg: sty.Bg})
			if r.Kind == RowWorkspace {
				f.Set(ix, y, " ", sty)
			} else if r.Kind == RowGit {
				f.Fill(ix, y, min(3, iw), 1, " ", sty)
			}
		}
		if active || dragged || selected {
			sty.Fg = f.Palette.Text
			sty.Bg = f.Palette.PanelBG
			if selected && !in.ServerUI {
				sty.Bg = f.Palette.Surface
			}
			if dragged && !in.ServerUI {
				sty.Bg = f.Palette.NavigatorDivider
			}
			sty.Attrs = uv.AttrBold
			fill := uv.Style{Bg: sty.Bg}
			if in.ServerUI {
				sty.Attrs = 0
				fill.Fg = f.Palette.Text
			}
			f.Fill(ix, y, iw, 1, " ", fill)
			if active || selected {
				f.Set(ix, y, "▌", uv.Style{Fg: f.Palette.Accent, Bg: sty.Bg})
			}
		}
		if r.Kind == RowCardBlank {
			return
		}
		if r.Kind == RowGit {
			gitStyle := sty
			if !active {
				gitStyle.Attrs &^= uv.AttrBold
			}
			gitX := ix + 3
			if in.ServerUI {
				for _, d := range in.JobDots[r.Ws] {
					gitX = f.Text(gitX, y, "●", uv.Style{Fg: f.ThemeColor(d.Color), Bg: sty.Bg}, ix+iw)
				}
				if len(in.JobDots[r.Ws]) > 0 {
					gitX = f.Text(gitX, y, " ", sty, ix+iw)
				}
			}
			g := in.Git[r.Ws]
			if in.ServerUI {
				g.Ahead, g.Behind, g.Added, g.Deleted = 0, 0, 0, 0
			}
			drawGitRow(f, gitX, y, max(0, ix+iw-gitX), g, gitStyle)
			return
		}
		x := ix + 1
		if ws.Worktree != nil && ws.Worktree.IsLinkedWorktree {
			x += 2
		}
		g := wsGlyph(s, r.Ws, in)
		if g.Rune == " " {
			g.Rune = "·"
		}
		x = f.Text(x, y, g.Rune, uv.Style{Fg: f.ThemeColor(g.Color), Bg: sty.Bg}, ix+iw)
		idSpace := uv.Style{Fg: f.Palette.Dim, Bg: sty.Bg, Attrs: uv.AttrFaint}
		x = f.Text(x, y, " ", idSpace, ix+iw)
		x = f.Text(x, y, fmt.Sprint(ws.ID), uv.Style{Fg: f.Palette.Dim, Bg: sty.Bg, Attrs: uv.AttrBold}, ix+iw)
		x = f.Text(x, y, " ", idSpace, ix+iw)
		name := in.WorkspaceNames[r.Ws]
		if name == "" || ws.CustomName {
			name = WorkspaceDisplayName(ws, in.Panes)
		}
		dots := in.JobDots[r.Ws]
		if in.ServerUI {
			dots = nil
		}
		f.Text(x, y, truncate(name, iw-(x-ix)-len(dots)), sty, ix+iw-len(dots))
		for i, d := range dots {
			f.Set(ix+iw-len(dots)+i, y, "●", uv.Style{Fg: f.ThemeColor(d.Color), Bg: sty.Bg})
		}
	case RowAgent:
		panel := in.SidebarConfig.UI.Sidebar.Agents
		rows := panel.Rows
		if specific, ok := panel.RowsByAgent[in.Panes[r.Pane].Agent]; ok {
			rows = specific
		}
		if hasConfiguredAgentRows(in.SidebarConfig, in.Panes[r.Pane].Agent) {
			rows = visibleAgentRows(s, in.Panes, r, rows)
		}
		if len(rows) > 0 {
			if r.Line < len(rows) {
				drawConfiguredAgent(f, s, in, r, rows[r.Line], ix, y, iw)
			}
			return
		}
		sn := in.Panes[r.Pane]
		if sn == nil {
			return
		}
		if in.ServerUI {
			base := uv.Style{Fg: f.Palette.Subtle, Bg: f.Palette.Surface}
			if r.Ws == s.Active && s.Workspaces[r.Ws].TabRef().Layout.Focused() == r.Pane {
				base.Fg, base.Bg = f.Palette.Text, f.Palette.PanelBG
			}
			f.Fill(ix, y, iw, 1, " ", uv.Style{Fg: f.Palette.Text, Bg: base.Bg})
			f.Set(ix, y, " ", base)
			if r.Line == 0 {
				name := in.WorkspaceNames[r.Ws]
				if name == "" || s.Workspaces[r.Ws].CustomName {
					name = WorkspaceDisplayName(s.Workspaces[r.Ws], in.Panes)
				}
				ws := s.Workspaces[r.Ws]
				tabLabel := ""
				for _, tab := range ws.Tabs {
					if tab.Layout.HasPane(r.Pane) && (len(ws.Tabs) > 1 || tab.Name != "") {
						label := tab.Name
						if label == "" {
							label = fmt.Sprint(tab.ID)
						}
						tabLabel = label
						break
					}
				}
				f.Text(ix+1, y, sn.Status.Rune, uv.Style{Fg: f.ThemeColor(sn.Status.Color), Bg: base.Bg}, ix+iw)
				f.Text(ix+2, y, " ", uv.Style{Fg: f.Palette.Dim, Bg: base.Bg, Attrs: uv.AttrFaint}, ix+iw)
				drawTokens(f, ix+3, y, max(0, iw-3), []styledToken{
					{name: "workspace", text: name, style: base},
					{name: "tab", text: tabLabel, style: uv.Style{Fg: f.Palette.Dim, Bg: base.Bg}},
				})
			} else {
				name := sn.DisplayAgent
				if name == "" {
					name = sn.Agent
				}
				f.Text(ix+1, y, truncate(name, max(0, iw-1)), uv.Style{Fg: f.Palette.Dim, Bg: base.Bg}, ix+iw)
			}
			return
		}

		drawConfiguredAgent(f, s, in, r, []config.SidebarToken{{Token: "workspace"}, {Token: "tab"}, {Token: "agent"}}, ix, y, iw)

	}
}
func WorkspaceDisplayName(ws *model.Workspace, panes map[int]*PaneSnap) string {
	if ws.CustomName && ws.Name != "" {
		return ws.Name
	}
	name := filepath.Base(ws.Cwd)
	if name == "." || name == "" {
		name = ws.Name
	}
	if ws.Worktree != nil {
		name = ws.Worktree.RepoName
	}
	if len(ws.Tabs) > 0 {
		ids := ws.Tabs[0].Layout.PaneIDs()
		if len(ids) > 0 {
			if p := panes[ids[0]]; p != nil && p.Agent != "" && paneTaskOrOSC(p) != "" && paneTaskOrOSC(p) != name {
				name += "-" + paneTaskOrOSC(p)
			}
		}
	}
	return name
}

// BudgetLabels reserves separator cells and apportions remaining display cells evenly.
func BudgetLabels(labels []string, width int) string {
	budget := make([]int, len(labels))
	remaining := width - 3*(len(labels)-1)
	if remaining < len(labels) {
		var out []string
		for i := len(labels) - 1; i >= 0; i-- {
			w := ansi.StringWidth(labels[i])
			if w <= width {
				out = append([]string{labels[i]}, out...)
				width -= w + 3
			}
		}
		return strings.Join(out, " · ")
	}
	for i := range budget {
		budget[i] = 1
		remaining--
	}
	for remaining > 0 {
		grew := false
		for i, label := range labels {
			if budget[i] < ansi.StringWidth(label) && remaining > 0 {
				budget[i]++
				remaining--
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	var out []string
	for i, label := range labels {
		out = append(out, truncate(label, budget[i]))
	}
	return strings.Join(out, " · ")
}

// truncate cuts s to at most w display cells, ending with "…" when cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runeW(r)
		if cur+rw+1 > w {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String() + "…"
}

func runeW(r rune) int {
	return ansi.StringWidth(string(r))
}

// wsGlyph aggregates the pane glyphs of a workspace into one attention glyph.
func wsGlyph(s *model.AppState, wi int, in Input) StatusGlyph {
	return aggregateGlyph(s.Workspaces[wi], in)
}

// aggregateGlyph picks the most attention-worthy pane glyph of a workspace.
func aggregateGlyph(ws *model.Workspace, in Input) StatusGlyph {
	best := StatusGlyph{Rune: " ", Color: Dim}
	for _, t := range ws.Tabs {
		if g := tabGlyph(t, in); glyphRank(g) > glyphRank(best) {
			best = g
		}
	}
	return best
}

// workingRune returns the spinner frame for the working aggregate glyph
// (SPEC G6: working indicators spin instead of a fixed dot).
func workingRune(spin int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if spin < 0 {
		spin = 0
	}
	return frames[spin%len(frames)]
}

// glyphRank orders state keys by attention: blocked > working > done > idle
// > unknown.
func glyphRank(g StatusGlyph) int {
	switch g.Key {
	case "blocked":
		return 4
	case "working":
		return 3
	case "done":
		return 2
	case "idle":
		return 1
	}
	return 0
}
