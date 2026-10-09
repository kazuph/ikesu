package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// Rows are shared by drawing and tapping; list headings have no hit action.
func MobileRows(s *model.AppState, menu []MenuItem) []SidebarRow {
	rows := []SidebarRow{{Kind: RowMobileSpaces}, {Kind: RowMobileNewWorkspace}}
	for _, sec := range model.SectionOrder {
		found := false
		for i, w := range s.Workspaces {
			if w.SidebarSection() == sec && !s.WorktreeHidden(i) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		rows = append(rows, SidebarRow{Kind: RowSection, Section: sec})
		if s.CollapsedSections[sec] {
			continue
		}
		for i, w := range s.Workspaces {
			if w.SidebarSection() == sec && !s.WorktreeHidden(i) {
				rows = append(rows, SidebarRow{Kind: RowWorkspace, Ws: i}, SidebarRow{Kind: RowGit, Ws: i})
			}
		}
	}
	rows = append(rows, SidebarRow{Kind: RowMobileTabs}, SidebarRow{Kind: RowMobileNewTab})
	if ws := s.ActiveWorkspace(); ws != nil {
		for i := range ws.Tabs {
			rows = append(rows, SidebarRow{Kind: RowMobileTab, Line: i})
		}
	}
	rows = append(rows, SidebarRow{Kind: RowMobileMenu})
	for i := range menu {
		rows = append(rows, SidebarRow{Kind: RowMobileMenuItem, Line: i})
	}
	return rows
}

func MobileSwitchRect(s *model.AppState) layout.Rect {
	// Observed distribution header reserves its rightmost ten columns for switch.
	return layout.Rect{X: max(0, s.Cols-10), Y: 0, W: min(10, s.Cols), H: min(2, s.Rows)}
}

func MobileViewport(s *model.AppState) layout.Rect {
	return layout.Rect{X: 0, Y: 3, W: s.Cols, H: max(0, s.Rows-3)}
}

func MobileOffset(s *model.AppState, menu []MenuItem, offset int) int {
	return max(0, min(offset, max(0, len(MobileRows(s, menu))-MobileViewport(s).H)))
}

func drawMobile(f *Frame, in Input, out *Output) {
	s := in.State
	header := uv.Style{Bg: f.Palette.SurfDim}
	if in.ServerUI {
		header.Fg = f.Palette.Text
	}
	f.Fill(0, 0, s.Cols, min(2, s.Rows), " ", header)
	switcher := MobileSwitchRect(s)
	switchFill := uv.Style{Bg: f.Palette.Surface}
	if in.ServerUI {
		switchFill.Fg = f.Palette.Text
	}
	f.Fill(switcher.X, 0, switcher.W, min(2, s.Rows), " ", switchFill)
	for y := 0; y < min(2, s.Rows); y++ {
		f.Set(switcher.X, y, "│", uv.Style{Fg: f.Palette.PanelBG, Bg: f.Palette.Surface})
	}
	name := "spaces"
	if w := s.ActiveWorkspace(); w != nil {
		name = in.WorkspaceNames[s.Active]
		if name == "" || w.CustomName {
			name = WorkspaceDisplayName(w, in.Panes)
		}
	}
	if in.MobileSwitcher {
		f.Fill(switcher.X+1, 0, max(0, switcher.W-1), 1, " ", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.Surface, Attrs: uv.AttrBold})
		f.Fill(switcher.X+1, 1, max(0, switcher.W-1), 1, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold})
		f.Fill(0, 0, switcher.X, 1, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold})
		f.Text(0, 0, " switch", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, switcher.X)
		f.Text(switcher.X+1, 0, "  close", uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, s.Cols)
		f.Text(switcher.X+1, 1, "    ×", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, s.Cols)
	} else {
		f.Fill(switcher.X+1, 1, max(0, switcher.W-1), 1, " ", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold})
		f.Text(switcher.X+1, 1, " switch", uv.Style{Fg: f.Palette.Text, Bg: f.Palette.Surface, Attrs: uv.AttrBold}, s.Cols)
		glyph := wsGlyph(s, s.Active, in)
		if glyph.Rune == " " {
			glyph.Rune = "·"
		}
		f.Text(1, 0, glyph.Rune, uv.Style{Fg: f.ThemeColor(glyph.Color), Bg: f.Palette.SurfDim}, switcher.X)
		f.Text(3, 0, truncate(name, max(0, switcher.X-3)), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, switcher.X)
		if w := s.ActiveWorkspace(); w != nil {
			label := fmt.Sprintf(" tab %d", w.ActiveTab+1)
			if len(w.Tabs) > 1 {
				label += fmt.Sprintf(" · %d/%d", w.ActiveTab+1, len(w.Tabs))
			}
			f.Text(switcher.X-ansi.StringWidth(label)-1, 0, label, uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.SurfDim}, switcher.X)
		}
		counts := map[string]int{}
		for _, pane := range in.Panes {
			if pane != nil && pane.Status.Key != "" && pane.Status.Key != "unknown" {
				counts[pane.Status.Key]++
			}
		}
		summary := "no agents"
		primary := ""
		style := uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.SurfDim}
		switch {
		case counts["blocked"] > 0:
			primary = "blocked"
			summary = fmt.Sprintf("◉ %d blocked", counts["blocked"])
			style.Fg, style.Attrs = f.Palette.Red, uv.AttrBold
		case counts["working"] > 0:
			primary = "working"
			summary = fmt.Sprintf("%d working", counts["working"])
			style.Fg, style.Attrs = f.Palette.Yellow, uv.AttrBold
		case counts["done"] > 0:
			primary = "done"
			summary = fmt.Sprintf("● %d", counts["done"])
			style.Fg, style.Attrs = f.Palette.Green, uv.AttrBold
			if in.ServerUI {
				summary += " done"
				style.Fg = f.Palette.Accent
			}
		case counts["idle"] > 0:
			primary = "idle"
			summary = "all idle"
		}
		x := f.Text(1, 1, summary, style, switcher.X)
		if in.ServerUI {
			for _, key := range []string{"blocked", "working", "done", "idle"} {
				if key == primary || counts[key] == 0 {
					continue
				}
				x = f.Text(x, 1, " · ", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.SurfDim}, switcher.X)
				x = f.Text(x, 1, fmt.Sprintf("%d %s", counts[key], key), uv.Style{Fg: f.Palette.Overlay1, Bg: f.Palette.SurfDim}, switcher.X)
			}
			if counts["blocked"] > 0 {
				f.Set(s.Cols-1, 0, "●", uv.Style{Fg: f.Palette.Red, Bg: f.Palette.Surface})
			}
		}
	}
	if !in.MobileSwitcher {
		return
	}
	out.AnchorVisible = false
	viewport := MobileViewport(s)
	f.Fill(0, 2, s.Cols, max(0, s.Rows-2), " ", uv.Style{Bg: f.Palette.SurfDim})
	f.Text(0, 2, stringRule(s.Cols), uv.Style{Fg: f.Palette.PanelBG, Bg: f.Palette.SurfDim}, s.Cols)
	rows := MobileRows(s, in.MobileMenu)
	off := MobileOffset(s, in.MobileMenu, in.MobileScroll)
	for n := 0; n < viewport.H && n+off < len(rows); n++ {
		row, y := rows[n+off], viewport.Y+n
		bg := f.Palette.SurfDim
		if (row.Kind == RowWorkspace || row.Kind == RowGit) && row.Ws == s.Active {
			bg = f.Palette.Surface
		}
		if row.Kind == RowMobileTab && s.ActiveWorkspace() != nil && row.Line == s.ActiveWorkspace().ActiveTab {
			bg = f.Palette.PanelBG
		}
		f.Fill(1, y, max(0, s.Cols-1), 1, " ", uv.Style{Bg: bg})
		switch row.Kind {
		case RowMobileSpaces, RowMobileTabs, RowMobileMenu, RowSection:
			label := " spaces"
			switch row.Kind {
			case RowMobileTabs:
				label = " tabs"
			case RowMobileMenu:
				label = " menu"
			case RowSection:
				arrow := "▾"
				if s.CollapsedSections[row.Section] {
					arrow = "▸"
				}
				label = " " + arrow + " " + row.Section.Label()
			}
			style := uv.Style{Fg: f.Palette.Overlay1, Bg: bg, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}
			f.Fill(1, y, max(0, s.Cols-2), 1, " ", style)
			f.Text(1, y, truncate(label, max(0, s.Cols-2)), style, max(1, s.Cols-1))
		case RowMobileNewWorkspace, RowMobileNewTab:
			label := "  + new workspace"
			if row.Kind == RowMobileNewTab {
				label = "  + new tab"
			}
			style := uv.Style{Fg: f.Palette.Accent, Bg: bg, Attrs: uv.AttrBold}
			f.Fill(1, y, max(0, s.Cols-1), 1, " ", style)
			f.Text(1, y, label, style, s.Cols)
		case RowWorkspace:
			glyph := wsGlyph(s, row.Ws, in)
			if glyph.Rune == " " {
				glyph.Rune = "·"
			}
			f.Text(3, y, glyph.Rune, uv.Style{Fg: f.ThemeColor(glyph.Color), Bg: bg}, s.Cols)
			name := in.WorkspaceNames[row.Ws]
			if name == "" || s.Workspaces[row.Ws].CustomName {
				name = WorkspaceDisplayName(s.Workspaces[row.Ws], in.Panes)
			}
			f.Text(5, y, truncate(name, max(0, s.Cols-5)), uv.Style{Fg: f.Palette.Text, Bg: bg, Attrs: uv.AttrBold}, s.Cols)
		case RowGit:
			ws := s.Workspaces[row.Ws]
			branch := in.Git[row.Ws].Branch
			if branch == "" {
				branch = "shell"
			}
			label := fmt.Sprintf("  %s · tab %d", branch, ws.ActiveTab+1)
			if len(ws.Tabs) > 1 {
				label += fmt.Sprintf(" · %d/%d", ws.ActiveTab+1, len(ws.Tabs))
			}
			style := uv.Style{Fg: f.Palette.Dim, Bg: bg}
			f.Fill(1, y, max(0, s.Cols-1), 1, " ", style)
			f.Text(1, y, truncate(label, max(0, s.Cols-1)), style, s.Cols)
		case RowMobileTab:
			ws := s.ActiveWorkspace()
			name := ws.Tabs[row.Line].Name
			if name == "" {
				name = fmt.Sprintf("tab %d", row.Line+1)
			}
			f.Text(3, y, truncate(name, max(0, s.Cols-3)), uv.Style{Fg: f.Palette.Text, Bg: bg, Attrs: uv.AttrBold}, s.Cols)
		case RowMobileMenuItem:
			item := in.MobileMenu[row.Line]
			label := item.Label
			if item.Separator {
				label = "--"
			}
			style := uv.Style{Fg: f.Palette.Overlay1, Bg: bg}
			f.Fill(1, y, max(0, s.Cols-1), 1, " ", style)
			f.Text(1, y, "  "+truncate(label, max(0, s.Cols-3)), style, s.Cols)
		}
	}
	if len(rows) > viewport.H && viewport.H > 0 {
		// Cell coverage is rounded upward, with the start at the proportional row.
		height := (viewport.H*viewport.H + len(rows) - 1) / len(rows)
		top := off * viewport.H / len(rows)
		for n := 0; n < viewport.H; n++ {
			glyph, fg := "│", f.Palette.PanelBG
			if n >= top && n < top+height {
				glyph, fg = "▌", f.Palette.Accent
			}
			f.Set(0, viewport.Y+n, glyph, uv.Style{Fg: fg, Bg: f.Palette.SurfDim})
		}
	}

}

func stringRule(width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s += "─"
	}
	return s
}
