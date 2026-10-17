package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/layout"
	"math"
	"strings"
)

type NavigatorRow struct {
	Workspace, Tab, Pane, Count, Depth          int
	Name, WorkspaceName, TabName, Agent, Status string
	Current                                     bool
}

type Navigator struct {
	Rows                    []NavigatorRow
	Total, Selected, Scroll int
	Query, Filter           string
	Searching               bool
}

// Margins measured at 40/80/100/120/160/165 columns and 25/30/31/45/47 rows.
func NavigatorRect(cols, rows int) layout.Rect {
	x, y := cols/16, rows/10
	return layout.Rect{X: x, Y: y, W: cols - 2*x, H: rows - 2*y}
}

func drawNavigator(f *Frame, n *Navigator, spin int) {
	r := NavigatorRect(f.W, f.H)
	normal := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
	dim := uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}
	accent := uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG}
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Box(r.X, r.Y, r.W, r.H, accent)
	header := "search panes"
	if n.Query != "" {
		header = n.Query
	}
	filters := map[string]string{"blocked": "◉ blocked", "working": workingRune(spin) + " working", "idle": "✓ idle", "done": "● done"}
	if n.Filter != "" {
		header = filters[n.Filter]
	}
	f.Fill(r.X+1, r.Y+1, r.W-2, 1, " ", dim)
	f.Text(r.X+1, r.Y+1, " / "+header, dim, r.X+r.W-1)
	if n.Searching {
		searchStyle := accent
		searchStyle.Attrs = uv.AttrBold
		f.Text(r.X+1, r.Y+1, " / ", searchStyle, r.X+r.W-1)
	}
	if n.Query != "" && n.Filter == "" {
		f.Text(r.X+4, r.Y+1, n.Query, normal, r.X+r.W-1)
	}
	if n.Filter != "" {
		filterStyle := uv.Style{Fg: f.Palette.Red, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}
		switch n.Filter {
		case "working":
			filterStyle.Fg = f.Palette.Yellow
		case "idle":
			filterStyle.Fg = f.Palette.Green
		case "done":
			filterStyle.Fg = f.Palette.NavigatorDone
		}
		f.Text(r.X+4, r.Y+1, header, filterStyle, r.X+r.W-1)
		f.Set(r.X+5, r.Y+1, " ", uv.Style{Bg: f.Palette.PopupBG})
	}
	count := fmt.Sprint(n.Total)
	countX := r.X + r.W - len(count) - 2
	if n.Filter != "" || n.Query != "" {
		// Query lengths 1..15 move this column by one cell per character.
		countX += ansi.StringWidth(header) - ansi.StringWidth("search panes")
		count = fmt.Sprintf("%d panes", n.Total)
	}
	countEnd := f.Text(countX, r.Y+1, count, dim, r.X+r.W-1)
	if n.Filter != "" || n.Query != "" {
		f.Fill(countEnd, r.Y+1, r.X+r.W-1-countEnd, 1, " ", uv.Style{Bg: f.Palette.PopupBG})
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		f.Set(x, r.Y+2, "─", uv.Style{Fg: f.Palette.NavigatorDivider, Bg: f.Palette.PopupBG})
		footerStyle := dim
		if len(n.Rows) == 0 {
			footerStyle.Fg = f.Palette.NavigatorDivider
		}
		f.Set(x, r.Y+r.H-3, "─", footerStyle)
	}
	for i := n.Scroll; i < len(n.Rows) && i-n.Scroll < r.H-6; i++ {
		row := n.Rows[i]
		y := r.Y + 3 + i - n.Scroll
		sty := normal
		if i == n.Selected {
			sty = uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent}
		}
		f.Fill(r.X+1, y, r.W-2, 1, " ", sty)
		selected, current := " ", " "
		if i == n.Selected {
			selected = "→"
		}
		if row.Current {
			current = "◆"
		}

		prefixStyle, nameStyle := dim, normal
		if i == n.Selected {
			prefixStyle = sty
			nameStyle = sty
			nameStyle.Attrs = uv.AttrBold
		} else if row.Current {
			nameStyle.Attrs = uv.AttrBold
		} else {
			nameStyle.Fg = f.Palette.Subtle
		}
		prefix := fmt.Sprintf("%s├─ %s %s %s", strings.Repeat(" ", 1+2*row.Depth), selected, current, row.Status)
		name := row.Name
		if row.Pane == -1 {
			prefix = fmt.Sprintf(" ▾ %s %s %s", selected, current, row.Status)
			if n.Query == "" && n.Filter == "" && (i+1 == len(n.Rows) || n.Rows[i+1].Workspace != row.Workspace) {
				prefix = fmt.Sprintf(" ▸ %s %s %s", selected, current, row.Status)
			}
			name = fmt.Sprintf("%s (%d)", row.Name, row.Count)
		}
		processMargin := 0
		// The measured process column changes at these panel widths.
		switch {
		case r.W >= 92:
			processMargin = 28
		case r.W >= 70:
			processMargin = 20
		case r.W >= 54:
			processMargin = 14
		}
		stop := r.X + r.W - 1
		if processMargin > 0 {
			stop = r.X + r.W - processMargin - 1
		}
		next := f.Text(r.X+1, y, prefix, prefixStyle, stop)
		if i == n.Selected && next > r.X+1 {
			status := prefixStyle
			status.Attrs = uv.AttrBold
			f.Set(next-1, y, row.Status, status)
		}
		f.Text(next+1, y, name, nameStyle, stop)
		if processMargin > 0 {
			processStyle := dim
			if i == n.Selected {
				processStyle = sty
			}
			f.Fill(stop, y, r.X+r.W-1-stop, 1, " ", processStyle)
			if row.Pane != -1 {
				f.Text(stop+1, y, row.Agent, processStyle, r.X+r.W-1)
			}
		}

	}
	if len(n.Rows) > max(0, r.H-6) {
		body := r.H - 6
		thumb := max(1, int(math.Round(float64(body*body)/float64(len(n.Rows)))))
		top := int(math.Round(float64(n.Scroll) / float64(len(n.Rows)-body) * float64(body-thumb)))
		for y := r.Y + 3; y < r.Y+r.H-3; y++ {
			style := uv.Style{Fg: f.Palette.PanelBG, Bg: f.Palette.PopupBG}
			if y-(r.Y+3) >= top && y-(r.Y+3) < top+thumb {
				style.Fg = f.Palette.Dim
			}
			if y-(r.Y+3)+n.Scroll == n.Selected {
				style.Bg = f.Palette.Accent
			}
			f.Set(r.X+r.W-2, y, "▕", style)
		}
	}
	if len(n.Rows) > 0 {
		row := n.Rows[n.Selected]
		label := fmt.Sprintf(" %s · %s · %s", row.WorkspaceName, row.Name, row.Agent)
		if row.Pane == -1 {
			label = fmt.Sprintf(" %s · %d panes", row.Name, row.Count)
		}
		if row.Pane == -2 {
			label = fmt.Sprintf(" %s · tab: %s · %d panes · %s", row.WorkspaceName, row.TabName, row.Count, row.Agent)
		} else if row.TabName != "" {
			label = fmt.Sprintf(" %s · tab: %s · %s · %s", row.WorkspaceName, row.TabName, row.Name, row.Agent)
		}
		f.Text(r.X+1, r.Y+r.H-3, label, dim, r.X+r.W-1)
	}

	parts := []string{" enter", " switch  ", "/", " search  ", "b/w/i/d/a", " states  ", "j/k/↑↓", " move  ", "esc", " close"}
	if n.Searching {
		parts = []string{" enter", " switch  ", "↑↓", " move  ", "ctrl+u", " clear  ", "esc", " back"}
	}
	x := r.X + 1
	for i, text := range parts {
		style := dim
		if i%2 == 0 {
			style = accent
			style.Attrs = uv.AttrBold
		}
		x = f.Text(x, r.Y+r.H-2, text, style, r.X+r.W-1)
	}

}
