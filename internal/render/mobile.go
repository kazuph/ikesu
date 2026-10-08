package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// Rows are shared by drawing and tapping. Blank header rows have no hit action.
func MobileRows(s *model.AppState) []SidebarRow {
	var rows []SidebarRow
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
		rows = append(rows, SidebarRow{Kind: RowSection, Section: sec}, SidebarRow{Kind: RowBlank})
		if s.CollapsedSections[sec] {
			continue
		}
		for i, w := range s.Workspaces {
			if w.SidebarSection() == sec && !s.WorktreeHidden(i) {
				rows = append(rows, SidebarRow{Kind: RowWorkspace, Ws: i}, SidebarRow{Kind: RowGit, Ws: i})
			}
		}
	}
	return rows
}

func MobileSwitchRect(s *model.AppState) layout.Rect {
	// Observed distribution header reserves its rightmost ten columns for switch.
	return layout.Rect{X: max(0, s.Cols-10), Y: 0, W: min(10, s.Cols), H: min(2, s.Rows)}
}

func MobileViewport(s *model.AppState) layout.Rect {
	return layout.Rect{X: 0, Y: 3, W: s.Cols, H: max(0, s.Rows-4)}
}

func MobileOffset(s *model.AppState, offset int) int {
	return max(0, min(offset, max(0, len(MobileRows(s))-MobileViewport(s).H)))
}

func drawMobile(f *Frame, in Input, out *Output) {
	s := in.State
	header := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.SurfDim}
	f.Fill(0, 0, s.Cols, min(2, s.Rows), " ", header)
	switcher := MobileSwitchRect(s)
	label := " switch"
	if in.MobileSwitcher {
		label = " close"
	}
	f.Text(switcher.X, 1, label, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, s.Cols)
	name := "spaces"
	if w := s.ActiveWorkspace(); w != nil {
		name = in.WorkspaceNames[s.Active]
		if name == "" {
			name = WorkspaceDisplayName(w, in.Panes)
		}
	}
	f.Text(1, 0, truncate(name, max(0, switcher.X-2)), header, switcher.X)
	f.Text(1, 1, " agents", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.SurfDim}, switcher.X)
	if !in.MobileSwitcher {
		return
	}
	out.AnchorVisible = false
	viewport := MobileViewport(s)
	f.Fill(0, 2, s.Cols, max(0, s.Rows-3), " ", uv.Style{Bg: f.Palette.SurfDim})
	f.Text(0, 2, stringRule(s.Cols), uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.SurfDim}, s.Cols)
	rows := MobileRows(s)
	off := MobileOffset(s, in.MobileScroll)
	for n := 0; n < viewport.H && n+off < len(rows); n++ {
		row, y := rows[n+off], viewport.Y+n
		switch row.Kind {
		case RowSection:
			arrow := "▾"
			if s.CollapsedSections[row.Section] {
				arrow = "▸"
			}
			f.Text(0, y, truncate(arrow+" "+row.Section.Label(), s.Cols), uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, s.Cols)
		case RowWorkspace:
			glyph := wsGlyph(s, row.Ws, in)
			if glyph.Rune == " " {
				glyph.Rune = "·"
			}
			x := f.Text(2, y, glyph.Rune, uv.Style{Fg: f.ThemeColor(glyph.Color), Bg: f.Palette.SurfDim}, s.Cols)
			x = f.Text(x, y, fmt.Sprintf(" %d ", row.Ws+1), uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, s.Cols)
			name := in.WorkspaceNames[row.Ws]
			if name == "" {
				name = WorkspaceDisplayName(s.Workspaces[row.Ws], in.Panes)
			}
			f.Text(x, y, truncate(name, max(0, s.Cols-x)), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.SurfDim, Attrs: uv.AttrBold}, s.Cols)
		case RowGit:
			drawMobileGit(f, 4, y, max(0, s.Cols-4), in.Git[row.Ws], row.Ws == s.Active)
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

func drawMobileGit(f *Frame, x, y, width int, g GitStatus, active bool) {
	fg := f.Palette.Dim
	if active {
		fg = f.Palette.Mauve
	}
	style := uv.Style{Fg: fg, Bg: f.Palette.SurfDim}
	labels := ""
	for _, v := range []struct {
		mark string
		n    int
	}{{"↑", g.Ahead}, {"↓", g.Behind}, {"+", g.Added}, {"-", g.Deleted}} {
		if v.n > 0 {
			labels += fmt.Sprintf("%s%d ", v.mark, v.n)
		}
	}
	limit := x + width
	x = f.Text(x, y, labels, style, limit)
	branch := g.Branch
	if branch == "" {
		branch = "shell"
	}
	f.Text(x, y, truncate(branch, max(0, limit-x)), style, limit)
}
