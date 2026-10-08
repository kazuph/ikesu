package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
	"strconv"
	"strings"
)

type styledToken struct {
	name, text string
	style      uv.Style
	fixed      bool
}

func tokenStyle(t config.SidebarToken, style uv.Style) uv.Style {
	if t.Fg != nil {
		hex := (*t.Fg)[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		n, _ := strconv.ParseUint(hex, 16, 32)
		style.Fg = color.RGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}
	}
	if t.Bold != nil {
		style.Attrs &^= uv.AttrBold
		if *t.Bold {
			style.Attrs |= uv.AttrBold
		}
	}
	if t.Dim != nil {
		style.Attrs &^= uv.AttrFaint
		if *t.Dim {
			style.Attrs |= uv.AttrFaint
		}
	}
	return style
}
func drawTokens(f *Frame, x, y, w int, tokens []styledToken) {
	var ts []styledToken
	for _, v := range tokens {
		if v.text != "" {
			ts = append(ts, v)
		}
	}
	if len(ts) == 0 || w <= 0 {
		return
	}
	sep := func(a, b styledToken) string {
		if a.name == "workspace" && b.name == "tab" {
			return " · "
		}
		return " "
	}
	budget := make([]int, len(ts))
	used := 0
	for i, t := range ts {
		if i > 0 {
			used += ansi.StringWidth(sep(ts[i-1], t))
		}
		if t.fixed {
			budget[i] = ansi.StringWidth(t.text)
		} else {
			budget[i] = 1
		}
		used += budget[i]
	}
	if used > w {
		for i, t := range ts {
			if !t.fixed {
				budget[i] = 0
			}
		}
		used = 0
		last := -1
		for i, t := range ts {
			if budget[i] > 0 {
				if last >= 0 {
					used += ansi.StringWidth(sep(ts[last], t))
				}
				used += budget[i]
				last = i
			}
		}

		cost := func() int {
			total, last := 0, -1
			for i, t := range ts {
				if budget[i] > 0 {
					if last >= 0 {
						total += ansi.StringWidth(sep(ts[last], t))
					}
					total += budget[i]
					last = i
				}
			}
			return total
		}
		for i := len(ts) - 1; i >= 0; i-- {
			if ts[i].fixed {
				continue
			}
			budget[i] = ansi.StringWidth(ts[i].text)
			if cost() > w {
				budget[i] = 0
			}
		}

	} else {
		for used < w {
			grew := false
			for i, t := range ts {
				if !t.fixed && budget[i] < ansi.StringWidth(t.text) && used < w {
					budget[i]++
					used++
					grew = true
				}
			}
			if !grew {
				break
			}
		}
	}
	last := -1
	limit := x + w
	for i, t := range ts {
		if budget[i] == 0 {
			continue
		}
		if last >= 0 {
			x = f.Text(x, y, sep(ts[last], t), uv.Style{Fg: Dim, Bg: t.style.Bg}, limit)
		}
		x = f.Text(x, y, truncate(t.text, budget[i]), t.style, limit)
		last = i
	}
}
func drawConfiguredAgent(f *Frame, s *model.AppState, in Input, r SidebarRow, configured []config.SidebarToken, x, y, w int) {
	sn := in.Panes[r.Pane]
	if sn == nil {
		return
	}
	ws := s.Workspaces[r.Ws]
	tab := ""
	for _, t := range ws.Tabs {
		if t.Layout.HasPane(r.Pane) {
			tab = t.TabLabel()
			break
		}
	}
	base := uv.Style{Fg: Text}
	var tokens []styledToken
	hasPane, hasState := false, false
	for _, t := range configured {
		if t.Token == "pane" {
			hasPane = true
		}
		if t.Token == "state_icon" {
			hasState = true
		}
	}
	if r.Line == 0 {
		if !hasState {
			tokens = append(tokens, styledToken{name: "state_icon", text: sn.Status.Rune, style: uv.Style{Fg: sn.Status.Color}, fixed: true})
		}
		if !hasPane {
			tokens = append(tokens, styledToken{name: "pane", text: fmt.Sprintf("%%%d", r.Pane), style: uv.Style{Fg: Text, Attrs: uv.AttrBold}, fixed: true})
		}
	}
	for _, t := range configured {
		v := styledToken{name: t.Token, style: base}
		switch t.Token {
		case "state_icon":
			v.text = sn.Status.Rune
			v.style.Fg = sn.Status.Color
			v.fixed = true
		case "state_text":
			v.text = sn.Status.Key
		case "pane":
			v.style.Attrs |= uv.AttrBold
			v.text = fmt.Sprintf("%%%d", r.Pane)
			v.fixed = true
		case "workspace":
			v.text = in.WorkspaceNames[r.Ws]
			if v.text == "" {
				v.text = WorkspaceDisplayName(ws, in.Panes)
			}
		case "tab":
			v.text = tab
		case "agent":
			v.text = sn.Agent
		case "terminal_title_stripped":
			v.text = detect.StripTerminalTitle(sn.Title)
		case "terminal_title":
			v.text = sn.Title
		default:
			if strings.HasPrefix(t.Token, "$") {
				v.text = sn.Tokens[strings.TrimPrefix(t.Token, "$")]
			}
		}
		v.style = tokenStyle(t, v.style)
		tokens = append(tokens, v)
	}
	drawTokens(f, x+1, y, max(0, w-1), tokens)
}
func drawConfiguredSpace(f *Frame, s *model.AppState, in Input, r SidebarRow, configured []config.SidebarToken, x, y, w int) {
	ws := s.Workspaces[r.Ws]
	base := uv.Style{Fg: Text}
	if r.Ws == s.Active || (in.Dragging && r.Ws == in.DraggedWorkspace) || (in.SelectedWorkspace != nil && *in.SelectedWorkspace == r.Ws) {
		base.Bg = Surface
		base.Attrs = uv.AttrBold
		f.Fill(x, y, w, 1, " ", base)
		f.Set(x, y, "▌", uv.Style{Fg: Accent, Bg: Surface})
	}
	var tokens []styledToken
	for _, t := range configured {
		v := styledToken{name: t.Token, style: base}
		switch t.Token {
		case "state_icon":
			g := wsGlyph(s, r.Ws, in)
			v.text = g.Rune
			if v.text == " " {
				v.text = "·"
			}
			v.style.Fg = g.Color
			v.fixed = true
		case "state_text":
			v.text = wsGlyph(s, r.Ws, in).Key
		case "workspace":
			v.text = in.WorkspaceNames[r.Ws]
			if v.text == "" {
				v.text = WorkspaceDisplayName(ws, in.Panes)
			}
			tokens = append(tokens, styledToken{name: "workspace_number", text: fmt.Sprint(r.Ws + 1), style: tokenStyle(t, uv.Style{Fg: Dim, Bg: base.Bg, Attrs: uv.AttrBold}), fixed: true})
		case "branch":
			v.text = in.Git[r.Ws].Branch
			if v.text == "" {
				v.text = "nogit"
			}
			v.style.Fg = Dim

		case "git_status":
			g := in.Git[r.Ws]
			for _, stat := range []struct {
				mark string
				n    int
				fg   color.Color
			}{{"↑", g.Ahead, Green}, {"↓", g.Behind, Red}, {"+", g.Added, Green}, {"-", g.Deleted, Red}} {
				if stat.n > 0 {
					tokens = append(tokens, styledToken{name: "git_status", text: fmt.Sprintf("%s%d", stat.mark, stat.n), style: tokenStyle(t, uv.Style{Fg: stat.fg, Bg: base.Bg}), fixed: true})
				}
			}
			continue

		default:
			if strings.HasPrefix(t.Token, "$") {
				v.text = in.WorkspaceTokens[r.Ws][strings.TrimPrefix(t.Token, "$")]
			}
		}
		v.style = tokenStyle(t, v.style)
		tokens = append(tokens, v)
	}

	if r.Kind == RowWorkspace {
		for _, dot := range in.JobDots[r.Ws] {
			tokens = append(tokens, styledToken{name: "job", text: "●", style: uv.Style{Fg: dot.Color, Bg: base.Bg}, fixed: true})
		}
	}
	drawTokens(f, x+1, y, max(0, w-1), tokens)
}
