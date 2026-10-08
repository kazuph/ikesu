package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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
)

type SidebarRow struct {
	Kind     SidebarRowKind
	Ws, Pane int
	Section  model.Section
}

func SidebarRows(s *model.AppState, panes map[int]*PaneSnap) []SidebarRow {
	rows := []SidebarRow{{Kind: RowTitle}, {Kind: RowRule}}
	for _, sec := range model.SectionOrder {
		var cards []SidebarRow
		count := 0
		for wi, w := range s.Workspaces {
			if w.SidebarSection() != sec || s.WorktreeHidden(wi) {
				continue
			}
			count++
			if !s.CollapsedSections[sec] {
				cards = append(cards, SidebarRow{Kind: RowWorkspace, Ws: wi})
				if w.Worktree == nil || !w.Worktree.IsLinkedWorktree {
					cards = append(cards, SidebarRow{Kind: RowGit, Ws: wi})
				}
			}
		}
		if count == 0 {
			continue
		}
		if len(rows) > 2 {
			rows = append(rows, SidebarRow{Kind: RowRule})
		}
		rows = append(rows, SidebarRow{Kind: RowSection, Section: sec}, SidebarRow{Kind: RowRule}, SidebarRow{Kind: RowBlank, Section: sec})
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
			return agentRank(panes[agents[i].Pane].Status.Key) < agentRank(panes[agents[j].Pane].Status.Key)
		})
	}
	rows = append(rows, SidebarRow{Kind: RowSep})
	return append(rows, agents...)
}
func agentRank(key string) int {
	switch key {
	case "blocked":
		return 0
	case "done":
		return 1
	case "working":
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
func SidebarFooter(area layout.Rect) layout.Rect {
	return layout.Rect{X: area.X, Y: area.Y + area.H - 2, W: max(0, area.W-1), H: 1}
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
	fr := SidebarFooter(s.SidebarArea())
	return layout.Rect{X: fr.X + 1, Y: fr.Y, W: 6, H: 1}
}
func SidebarNewRect(s *model.AppState) layout.Rect {
	fr := SidebarFooter(s.SidebarArea())
	return layout.Rect{X: fr.X + fr.W - 6, Y: fr.Y, W: 6, H: 1}
}

func drawSidebar(f *Frame, s *model.AppState, in Input) {
	area := s.SidebarArea()
	if area.W == 0 || area.H == 0 {
		return
	}
	if !s.SidebarVisible {
		drawSidebarCollapsed(f, s, in, area)
		return
	}
	border := uv.Style{Fg: Dim}
	for y := area.Y; y < area.Y+area.H; y++ {
		f.Set(area.X+area.W-1, y, "│", border)
	}
	ix, iy, iw, avail := SidebarContent(area)
	if iw < 1 {
		return
	}

	rows := SidebarRows(s, in.Panes)
	scroll := in.SidebarScroll
	if max := len(rows) - avail; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := scroll; i < len(rows) && i < scroll+avail; i++ {
		drawSidebarRow(f, s, in, rows[i], ix, iy+i-scroll, iw)
	}

	fr := SidebarFooter(area)
	if fr.W > 0 && fr.Y >= iy {
		f.Text(fr.X+1, fr.Y, "[menu]", uv.Style{Fg: Subtle}, fr.X+fr.W)
		f.Text(fr.X+fr.W-6, fr.Y, "[new]", uv.Style{Fg: Subtle}, fr.X+fr.W)
	}
	wr := SidebarWidthRect(s)
	if wr.W > 0 {
		f.Text(wr.X, wr.Y, s.PresetLabel(), uv.Style{Fg: Accent, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}, wr.X+wr.W)
	}
	f.Set(area.X+area.W-2, area.Y+area.H-1, "«", uv.Style{Fg: Dim})
	if in.Copied != "" && area.W > 1 && area.H > 0 {
		x, y, w := area.X, area.Y+area.H-1, area.W-1
		f.Fill(x, y, w, 1, " ", uv.Style{Bg: SurfDim})
		f.Text(x, y, truncate(in.Copied, w), uv.Style{Fg: Green, Bg: SurfDim, Attrs: uv.AttrBold}, x+w)
	}
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
	border := uv.Style{Fg: Dim}
	switch r.Kind {
	case RowTitle:
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
		style := uv.Style{Fg: Dim, Attrs: uv.AttrBold}
		arrow := "▾"
		if s.CollapsedSections[r.Section] {
			arrow = "▸"
		}
		if in.DragSection == r.Section {
			style = uv.Style{Fg: SurfDim, Bg: Accent, Attrs: uv.AttrBold}
			f.Fill(ix, y, iw, 1, " ", style)
		}
		limit := iw
		if iw >= 6 {
			limit -= 6
		}
		f.Text(ix, y, truncate(arrow+" "+r.Section.Label(), limit), style, ix+limit)
		nr := SectionNewRect(ix, y, iw)
		if nr.W > 0 {
			f.Text(nr.X, y, "[new]", uv.Style{Fg: Dim, Bg: style.Bg, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}, ix+iw)
		}
	case RowSep:
		f.Text(ix, y, " agents", border, ix+iw)
		scope := s.AgentScope
		if scope != "sort" {
			scope = "all"
		}
		label := "[" + scope + "]"
		f.Text(ix+iw-len(label), y, label, border, ix+iw)
	case RowWorkspace, RowGit:
		ws := s.Workspaces[r.Ws]
		active := r.Ws == s.Active
		dragged := in.Dragging && r.Ws == in.DraggedWorkspace
		sty := uv.Style{Fg: Text}
		if active || dragged {
			sty.Bg = Surface
			sty.Attrs = uv.AttrBold
			f.Fill(ix, y, iw, 1, " ", sty)
			f.Set(ix, y, "▌", uv.Style{Fg: Accent, Bg: Surface})
		}
		if r.Kind == RowGit {
			drawGitRow(f, ix+4, y, max(0, iw-4), in.Git[r.Ws], sty)
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
		x = f.Text(x, y, g.Rune, uv.Style{Fg: g.Color, Bg: sty.Bg}, ix+iw)
		x = f.Text(x, y, fmt.Sprintf(" %d ", r.Ws+1), uv.Style{Fg: Dim, Bg: sty.Bg, Attrs: uv.AttrBold}, ix+iw)
		name := in.WorkspaceNames[r.Ws]
		if name == "" {
			name = WorkspaceDisplayName(ws, in.Panes)
		}
		dots := in.JobDots[r.Ws]
		f.Text(x, y, truncate(name, iw-(x-ix)-len(dots)), sty, ix+iw-len(dots))
		for i, d := range dots {
			f.Set(ix+iw-len(dots)+i, y, "●", uv.Style{Fg: d.Color, Bg: sty.Bg})
		}
	case RowAgent:
		sn := in.Panes[r.Pane]
		if sn == nil {
			return
		}
		x := f.Text(ix+1, y, sn.Status.Rune, uv.Style{Fg: sn.Status.Color}, ix+iw)
		x = f.Text(x, y, fmt.Sprintf(" %%%d ", r.Pane), uv.Style{Fg: Text, Attrs: uv.AttrBold}, ix+iw)
		ws := s.Workspaces[r.Ws]
		tab := ""
		for _, t := range ws.Tabs {
			if t.Layout.HasPane(r.Pane) {
				tab = t.TabLabel()
				break
			}
		}
		parts := BudgetLabels([]string{WorkspaceDisplayName(ws, in.Panes), tab, sn.Agent}, max(0, ix+iw-x))
		f.Text(x, y, parts, uv.Style{Fg: Text}, ix+iw)
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
			if p := panes[ids[0]]; p != nil && p.Agent != "" && p.Title != "" && p.Title != name {
				name += "-" + p.Title
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

func drawSidebarCollapsed(f *Frame, s *model.AppState, in Input, area layout.Rect) {
	border := uv.Style{Fg: Dim}
	ix, iy, iw, ih := f.Box(area.X, area.Y, area.W, area.H, border)
	if iw < 1 || ih < 1 {
		return
	}
	avail := ih
	scroll := in.SidebarScroll
	if max := len(s.Workspaces) - avail; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := scroll; i < len(s.Workspaces) && i < scroll+avail; i++ {
		y := iy + i - scroll
		sty := uv.Style{Fg: Text}
		if i == s.Active {
			sty = uv.Style{Fg: Accent, Attrs: uv.AttrBold}
		}
		x := f.Text(ix, y, fmt.Sprintf("%d", i+1), sty, ix+iw)
		// Aggregate agent glyph for the workspace (highest priority state).
		g := aggregateGlyph(s.Workspaces[i], in)
		f.Text(x, y, g.Rune, uv.Style{Fg: g.Color, Attrs: boldIf(g.Bold)}, ix+iw)
	}
	// Expand button centred on the bottom border row.
	f.Set(area.X+(area.W-1)/2, area.Y+area.H-1, "»", uv.Style{Fg: Dim})
}

// aggregateGlyph picks the most attention-worthy pane glyph of a workspace.
func aggregateGlyph(ws *model.Workspace, in Input) StatusGlyph {
	best := StatusGlyph{Rune: " ", Color: Dim}
	rank := 0
	for _, t := range ws.Tabs {
		for _, pid := range t.Layout.PaneIDs() {
			snap := in.Panes[pid]
			if snap == nil {
				continue
			}
			r := glyphRank(snap.Status)
			if r > rank {
				rank = r
				best = snap.Status
			}
		}
	}
	return best
}

// workingRune returns the spinner frame for the working aggregate glyph
// (SPEC G6: working indicators spin instead of a fixed dot).
func workingRune(spin int) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	if spin < 0 {
		spin = 0
	}
	return frames[spin%len(frames)]
}

// glyphRank orders state keys by attention: blocked > done > working > idle
// > unknown.
func glyphRank(g StatusGlyph) int {
	switch g.Key {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
}
