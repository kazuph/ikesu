package render

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/layout"
)

// Approval is client-owned presentation of an existing pending decision.
type Approval struct {
	Pane   int
	Title  string
	Lines  []string
	Scroll int
	Late   bool
	Kinds  []ApprovalLineKind
}

type ApprovalLineKind struct {
	Role     string
	Prefix   int
	Selected bool
}

func drawApprovalIndicators(f *Frame, in Input, out *Output) {
	if len(in.ApprovalCounts) == 0 {
		return
	}
	out.AnswerRects = make(map[int]layout.Rect)
	for _, pane := range in.State.DisplayPaneRects() {
		count := in.ApprovalCounts[pane.ID]
		if count == 0 || pane.Rect.W < 1 || pane.Rect.H < 1 {
			continue
		}
		label := fmt.Sprintf("▲ Answer (%d)", count)
		x, y := pane.Rect.X, pane.Rect.Y+pane.Rect.H-1
		limit := pane.Rect.X + pane.Rect.W
		if !in.State.PaneBorders {
			r, ok := approvalBorderlessSlot(f, in, out, ansi.StringWidth(label))
			if !ok {
				continue
			}
			x, y, limit = r.X, r.Y, r.X+r.W
		}
		if in.State.PaneBorders && pane.Rect.W > 2 && pane.Rect.H > 2 {
			x++
		}
		width := min(ansi.StringWidth(label), limit-x)
		if width <= 0 {
			continue
		}
		bg := f.Palette.Accent
		if in.ApprovalGuardPanes[pane.ID] {
			bg = f.Palette.Red
		}
		style := uv.Style{Fg: f.Palette.SurfDim, Bg: bg, Attrs: uv.AttrBold}
		f.Text(x, y, label, style, x+width)
		out.AnswerRects[pane.ID] = layout.Rect{X: x, Y: y, W: width, H: 1}
	}
}

func approvalBorderlessSlot(f *Frame, in Input, out *Output, width int) (layout.Rect, bool) {
	blocked := []layout.Rect{out.ActionBar.Copy, out.ActionBar.Cycle, out.ActionBar.Rotate, out.ActionBar.Equalize, out.NewTabRect, out.TabLeftRect, out.TabRightRect}
	blocked = append(blocked, out.TabHits...)
	blocked = append(blocked, SidebarViewForInput(in).Workspace.Rect)
	for _, r := range out.AnswerRects {
		blocked = append(blocked, r)
	}
	areas := []layout.Rect{in.State.ActionBarArea(), in.State.TabBarArea()}
	sidebar := in.State.SidebarArea()
	if sidebar.W > 0 {
		sidebar.W--
	}
	for y := sidebar.Y + sidebar.H - 1; y >= sidebar.Y; y-- {
		empty := true
		for x := sidebar.X; x < sidebar.X+sidebar.W; x++ {
			c := f.Cell(x, y)
			if c != nil && c.Content != "" && c.Content != " " {
				empty = false
				break
			}
		}
		if empty {
			areas = append(areas, layout.Rect{X: sidebar.X, Y: y, W: sidebar.W, H: 1})
		}
	}
	for _, area := range areas {
		if area.H == 0 {
			continue
		}
		for x := area.X; x+width <= area.X+area.W; x++ {
			r := layout.Rect{X: x, Y: area.Y, W: width, H: 1}
			free := true
			for _, b := range blocked {
				if b.W > 0 && b.Y <= r.Y && r.Y < b.Y+b.H && r.X < b.X+b.W && r.X+r.W > b.X {
					free = false
					break
				}
			}
			if !free {
				continue
			}
			for i := x; i < x+width; i++ {
				c := f.Cell(i, area.Y)
				if c != nil && c.Content != "" && c.Content != " " {
					free = false
					break
				}
			}
			if free {
				return r, true
			}
		}
	}
	return layout.Rect{}, false
}

func drawApproval(f *Frame, in Input, out *Output) {
	var area layout.Rect
	for _, pane := range in.State.DisplayPaneRects() {
		if pane.ID == in.Approval.Pane {
			area = pane.Rect
			break
		}
	}
	if area.W < 4 || area.H < 4 {
		area = in.State.PanesArea()
		if area.W < 4 || area.H < 4 {
			return
		}
	}
	w, h := min(area.W, 72), min(area.H, len(in.Approval.Lines)+4)
	if h < 4 {
		h = 4
	}
	r := layout.Rect{X: area.X + (area.W-w)/2, Y: area.Y + (area.H-h)/2, W: w, H: h}
	color := f.Palette.Red
	if in.Approval.Late {
		color = f.Palette.Accent
	}
	border := uv.Style{Fg: color, Bg: f.Palette.PopupBG}
	x, y, width, height := f.Box(r.X, r.Y, r.W, r.H, border)
	f.Fill(x, y, width, height, " ", uv.Style{Bg: f.Palette.PopupBG})
	f.Text(x, y, in.Approval.Title, uv.Style{Fg: color, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, max(x, x+width-11))
	if width >= 11 {
		f.Text(x+width-11, y, " esc close ", uv.Style{Fg: f.Palette.SurfDim, Bg: f.Palette.Accent, Attrs: uv.AttrBold}, x+width)
	}
	start := max(0, min(in.Approval.Scroll, max(0, len(in.Approval.Lines)-(height-1))))
	for i, line := range in.Approval.Lines[start:] {
		if i >= height-1 {
			break
		}
		kind := ApprovalLineKind{}
		if start+i < len(in.Approval.Kinds) {
			kind = in.Approval.Kinds[start+i]
		}
		style := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
		switch kind.Role {
		case "subtitle":
			style.Fg = f.Palette.Overlay1
		case "dim", "footer":
			style.Fg = f.Palette.Dim
		case "surface":
			style.Bg, style.Attrs = f.Palette.Surface, uv.AttrBold
		case "input":
			style.Bg = f.Palette.Surface
		}
		if kind.Selected {
			style.Fg, style.Bg, style.Attrs = f.Palette.SurfDim, f.Palette.Accent, uv.AttrBold
		}
		if kind.Role == "surface" || kind.Role == "input" || kind.Selected {
			f.Fill(x+1, y+1+i, max(0, width-2), 1, " ", style)
		}
		f.Text(x, y+1+i, line, style, x+width)
		if kind.Role == "footer" {
			if at := strings.Index(line, "text t"); at >= 0 {
				pos := x + ansi.StringWidth(line[:at+len("text ")])
				f.Text(pos, y+1+i, "t", uv.Style{Fg: f.Palette.Text, Bg: style.Bg}, x+width)
			}
			for _, key := range []string{"1-", "space", "ctrl+u", "↵", "↑↓", "esc"} {
				if at := strings.Index(line, key); at >= 0 {
					end := at + len(key)
					if key == "1-" {
						for end < len(line) && line[end] >= '0' && line[end] <= '9' {
							end++
						}
					}
					pos := x + ansi.StringWidth(line[:at])
					f.Text(pos, y+1+i, line[at:end], uv.Style{Fg: f.Palette.Text, Bg: style.Bg}, x+width)
				}
			}
		}
		if kind.Prefix > 0 && !kind.Selected {
			prefix := uv.Style{Fg: f.Palette.Dim, Bg: style.Bg}
			f.Text(x, y+1+i, ansi.Truncate(line, kind.Prefix, ""), prefix, x+width)
		}
	}
	out.ApprovalRect = r
	out.AnchorVisible = false
}
