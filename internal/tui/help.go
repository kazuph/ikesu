package tui

import (
	"sort"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/render"
)

// helpView is the keybind-help overlay state (SPEC G6): opened with
// prefix+? or [menu]→Keybinds, scrollable, closed with esc/enter/q or a
// click outside the box.
type helpView struct {
	scroll   int
	rect     layout.Rect // last composed box for hit-testing
	dragging bool
	grab     int
}

// openHelp shows the keybind overlay.
func (a *App) openHelp() {
	a.help = &helpView{}
	a.dirty = true
}

// helpRows lists every command gtx implements, grouped like the herdr
// overlay: global → workspaces/tabs → panes. Configured [keys] bindings
// are expanded so remapped users see their actual keys.
func (a *App) helpRows() []render.HelpRow {
	return []render.HelpRow{
		{Group: true, Cmd: "global"},
		{Bind: "ctrl+b", Cmd: "prefix mode"},
		{Bind: "prefix+?", Cmd: "keybinds"},
		{Bind: "prefix+o", Cmd: "open notification target"},
		{Bind: "prefix+d", Cmd: "detach"},

		{Group: true, Cmd: "workspaces / tabs"},
		{Bind: "prefix+shift+n", Cmd: "new workspace"},
		{Bind: "prefix+shift+d", Cmd: "close workspace"},
		{Bind: "prefix+c", Cmd: "new tab"},
		{Bind: "prefix+,", Cmd: "rename tab"},
		{Bind: "prefix+&", Cmd: "close tab"},
		{Bind: "prefix+n", Cmd: "next workspace"},
		{Bind: "prefix+p", Cmd: "previous workspace"},
		{Bind: "prefix+1..9", Cmd: "switch workspace"},

		{Group: true, Cmd: "panes"},
		{Bind: bindKeys(a.splitVKeys, '%', 'v'), Cmd: "split vertical"},
		{Bind: bindKeys(a.splitHKeys, '"', '-'), Cmd: "split horizontal"},
		{Bind: "prefix+x", Cmd: "close pane"},
		{Bind: bindKeys(a.renameKeys, 'P'), Cmd: "rename pane"},
		{Bind: bindKeys(a.copyKeys, '[', ']'), Cmd: "copy mode"},
		{Bind: bindKeys(a.editKeys, 'e'), Cmd: "edit scrollback"},
		{Bind: "prefix+b", Cmd: "toggle sidebar"},
		{Bind: "prefix+h", Cmd: "focus pane left"},
		{Bind: "prefix+j", Cmd: "focus pane down"},
		{Bind: "prefix+k", Cmd: "focus pane up"},
		{Bind: "prefix+l", Cmd: "focus pane right"},
		{Bind: "prefix+tab", Cmd: "cycle pane next"},
		{Bind: "prefix+shift+tab", Cmd: "cycle pane previous"},
		{Bind: bindKeys(a.historyBackKeys), Cmd: "focus history back"},
		{Bind: bindKeys(a.historyForwardKeys), Cmd: "focus history forward"},
	}
}

// bindKeys renders a binding set sorted, e.g. "prefix+% / prefix+v".
// A nil set uses defaults; an explicitly empty set is shown as unset.
func bindKeys(set map[rune]bool, fallback ...rune) string {
	keys := []rune{}
	if set == nil {
		keys = fallback
	} else {
		for r := range set {
			keys = append(keys, r)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	if len(keys) == 0 {
		return "unset"
	}
	out := ""
	for i, r := range keys {
		if i > 0 {
			out += " / "
		}
		name := string(r)
		if r == '-' {
			name = "minus"
		}
		if r >= 'A' && r <= 'Z' {
			name = "shift+" + string(r+'a'-'A')
		}
		out += "prefix+" + name
	}
	return out
}

// helpBody is the visible content-row count in the composed help box;
// must match render.drawHelp's geometry.
func (a *App) helpBody() int {
	hgt := len(a.helpRows()) + 4
	if max := a.st.Rows - 2; hgt > max {
		hgt = max
	}
	if hgt < 6 {
		hgt = 6
	}
	body := hgt - 4 // border×2 + subtitle + footer
	if body < 1 {
		body = 1
	}
	return body
}

func (a *App) helpMaxScroll() int {
	if m := len(a.helpRows()) - a.helpBody(); m > 0 {
		return m
	}
	return 0
}

func (a *App) helpScrollBy(d int) {
	h := a.help
	h.scroll += d
	if m := a.helpMaxScroll(); h.scroll > m {
		h.scroll = m
	}
	if h.scroll < 0 {
		h.scroll = 0
	}
	a.dirty = true
}

// helpKey handles keys while the overlay is open.
func (a *App) helpKey(k uv.Key) {
	switch {
	case k.Code == uv.KeyEscape || k.Code == uv.KeyEnter || k.Code == 'q':
		a.help = nil
	case k.Code == uv.KeyUp || k.Code == 'k':
		a.helpScrollBy(-1)
	case k.Code == uv.KeyDown || k.Code == 'j':
		a.helpScrollBy(1)
	case k.Code == uv.KeyPgUp:
		a.helpScrollBy(-a.helpBody())
	case k.Code == uv.KeyPgDown:
		a.helpScrollBy(a.helpBody())
	}
	a.dirty = true
}

// helpMouse routes mouse events while the overlay is open: wheel scrolls,
// a press inside is swallowed, a press outside closes the overlay and is
// consumed (SPEC G6 overlay capture).
func (a *App) helpMouse(ev input.Event) {
	m := ev.Mouse
	h := a.help
	body := a.helpBody()
	maxScroll := a.helpMaxScroll()
	if ev.Kind == input.KindMouseRelease {
		h.dragging = false
		a.dirty = true
		return
	}
	if ev.Kind == input.KindMouseMotion && h.dragging {
		top := m.Y - h.rect.Y - 2 - h.grab
		a.helpScrollBy(maxScroll - offFromThumb(body, maxScroll, top) - h.scroll)
		return
	}
	switch ev.Kind {
	case input.KindMouseWheel:
		if m.Button == uv.MouseWheelUp {
			a.helpScrollBy(-3)
		} else {
			a.helpScrollBy(3)
		}
	case input.KindMouseClick:
		r := a.help.rect
		if m.Button == uv.MouseLeft && maxScroll > 0 && m.X == r.X+r.W-2 && m.Y >= r.Y+2 && m.Y < r.Y+2+body {
			th, top := thumbGeom(body, maxScroll, maxScroll-h.scroll)
			rel := m.Y - r.Y - 2
			h.grab = rel - top
			if rel < top || rel >= top+th {
				h.grab = th / 2
				a.helpScrollBy(maxScroll - offFromThumb(body, maxScroll, rel-h.grab) - h.scroll)
			}
			h.dragging = true
			break
		}
		inside := m.X >= r.X && m.X < r.X+r.W && m.Y >= r.Y && m.Y < r.Y+r.H
		if !inside {
			a.help = nil
		}
	}
	a.dirty = true
}
