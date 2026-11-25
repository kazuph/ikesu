package render

import (
	"image/color"
	"strconv"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/rivo/uniseg"
)

// SidebarPanel is a block at the bottom of a sidebar, with one section
// per plugin and its output. Click a heading to fold its section.
type SidebarPanel struct {
	Sections []SidebarPanelSection
}

// SidebarPanelSection is one foldable section.
type SidebarPanelSection struct {
	Key, Title string
	Collapsed  bool
	Lines      []SidebarPanelLine
}

// SidebarPanelLine is a line of text; SGR colors in it are drawn.
type SidebarPanelLine struct {
	Text  string
	Error bool
}

// sidebarPanelMaxShare caps the panel so the spaces list keeps most rows.
const sidebarPanelMaxShare = 2

// SidebarPanelRows is how many rows the panel takes in a sidebar whose
// space list has room rows (headings first, lines cut when short of space).
func SidebarPanelRows(p *SidebarPanel, room int) int {
	if p == nil {
		return 0
	}
	limit := room / sidebarPanelMaxShare
	n := 0
	for _, sec := range p.Sections {
		n++
		if !sec.Collapsed {
			n += len(sec.Lines)
		}
	}
	return min(n, max(0, limit))
}

// SidebarPanelHeading is a clickable heading row.
type SidebarPanelHeading struct {
	Key  string
	Rect layout.Rect
}

// SidebarPanelLayout returns where each heading is drawn, for click tests.
func SidebarPanelLayout(s *model.AppState, p *SidebarPanel) []SidebarPanelHeading {
	return SidebarPanelHeadings(p, leftSidebarPanelRect(s))
}

func leftSidebarPanelRect(s *model.AppState) layout.Rect {
	r := SidebarFooter(s)
	r.H = s.SidebarPanelRows
	r.Y -= r.H
	return r
}

func RightSidebarPanelRect(s *model.AppState, p *SidebarPanel) layout.Rect {
	r := s.RightSidebarArea()
	if p == nil || r.W <= 1 || s.RightSidebarCompact() {
		return layout.Rect{}
	}
	rows := SidebarPanelRows(p, r.H-2) // the first two rows belong to the tabs
	return layout.Rect{X: r.X + 1, Y: r.Y + r.H - rows, W: r.W - 1, H: rows}
}

func RightSidebarListRect(s *model.AppState, p *SidebarPanel) layout.Rect {
	r := s.RightSidebarArea()
	rows := RightSidebarPanelRect(s, p).H
	return layout.Rect{X: r.X + 1, Y: r.Y + 2, W: max(0, r.W-1), H: max(0, r.H-2-rows)}
}

func SidebarPanelHeadings(p *SidebarPanel, r layout.Rect) []SidebarPanelHeading {
	if p == nil || r.H <= 0 {
		return nil
	}
	y, end := r.Y, r.Y+r.H
	var out []SidebarPanelHeading
	for _, sec := range p.Sections {
		if y >= end {
			break
		}
		out = append(out, SidebarPanelHeading{Key: sec.Key, Rect: layout.Rect{X: r.X, Y: y, W: r.W, H: 1}})
		y++
		if !sec.Collapsed {
			y += len(sec.Lines)
		}
	}
	return out
}

func drawSidebarPanel(f *Frame, s *model.AppState, in Input) {
	drawSidebarPanelAt(f, in.SidebarPanel, leftSidebarPanelRect(s))
}

func drawSidebarPanelAt(f *Frame, p *SidebarPanel, r layout.Rect) {
	if p == nil || r.H <= 0 {
		return
	}
	x, w := r.X, r.W
	y, end := r.Y, r.Y+r.H
	f.Fill(x, y, w, r.H, " ", uv.Style{})
	for _, sec := range p.Sections {
		if y >= end {
			return
		}
		drawSectionHeading(f, x, y, w, sec.Title, sec.Collapsed, false, false)
		y++
		if sec.Collapsed {
			continue
		}
		for _, line := range sec.Lines {
			if y >= end {
				return
			}
			base := uv.Style{Fg: f.Palette.Text}
			if line.Error {
				base = uv.Style{Fg: f.Palette.Dim}
			}
			drawANSIText(f, x+1, y, x+w, line.Text, base)
			y++
		}
	}
}

// drawANSIText draws text whose SGR sequences (colors, bold, dim) come from a
// plugin's output, clipped at limit.
func drawANSIText(f *Frame, x, y, limit int, text string, base uv.Style) {
	style := base
	for i := 0; i < len(text) && x < limit; {
		if text[i] == 0x1b {
			seq, _, n, _ := ansi.DecodeSequence(text[i:], ansi.NormalState, nil)
			if len(seq) >= 3 && strings.HasPrefix(seq, "\x1b[") && seq[len(seq)-1] == 'm' {
				params := seq[2 : len(seq)-1]
				if !strings.ContainsFunc(params, func(r rune) bool { return (r < '0' || r > '9') && r != ';' }) {
					style = applySGR(style, base, params)
				}
			}
			i += n
			continue
		}
		// 書記素クラスタ（結合文字・ZWJ の絵文字）を1つのセルにまとめて描く。
		next := len(text)
		if esc := strings.IndexByte(text[i:], 0x1b); esc >= 0 {
			next = i + esc
		}
		cluster, _, _, _ := uniseg.FirstGraphemeClusterInString(text[i:next], -1)
		if cluster == "" {
			i++
			continue
		}
		w := ansi.StringWidth(cluster)
		if x+max(w, 1) > limit {
			break
		}
		if w > 0 {
			f.Set(x, y, cluster, style)
			x += w
		}
		i += len(cluster)
	}
}

// applySGR applies one SGR parameter list ("1;38;2;217;119;87") to style.
func applySGR(style, base uv.Style, params string) uv.Style {
	parts := strings.Split(params, ";")
	if params == "" {
		parts = []string{"0"}
	}
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			style = base
		case n == 1:
			style.Attrs |= uv.AttrBold
		case n == 2:
			style.Attrs |= uv.AttrFaint
		case n == 22:
			style.Attrs &^= uv.AttrBold | uv.AttrFaint
		case n >= 30 && n <= 37:
			style.Fg = ansi.BasicColor(n - 30)
		case n >= 90 && n <= 97:
			style.Fg = ansi.BasicColor(n - 90 + 8)
		case n == 39:
			style.Fg = base.Fg
		case n == 38 && i+2 < len(parts) && parts[i+1] == "5":
			if c, err := strconv.Atoi(parts[i+2]); err == nil && c >= 0 && c < 256 {
				style.Fg = ansi.IndexedColor(c)
			}
			i += 2
		case n == 38 && i+4 < len(parts) && parts[i+1] == "2":
			r, _ := strconv.Atoi(parts[i+2])
			g, _ := strconv.Atoi(parts[i+3])
			b, _ := strconv.Atoi(parts[i+4])
			style.Fg = color.RGBA{uint8(r), uint8(g), uint8(b), 0xff}
			i += 4
		}
	}
	return style
}
