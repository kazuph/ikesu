package tui

import (
	"math"
	"sort"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
)

// helpView is the keybind-help overlay state (SPEC G6): opened with
// prefix+? or [menu]→Keybinds, scrollable, closed with esc/enter/? or a
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

// helpRows lists every command ikesu implements, grouped like the herdr
// overlay: global → workspaces/tabs → panes. Configured [keys] bindings
// are expanded so remapped users see their actual keys.
func (a *App) helpRows() []render.HelpRow {
	rows := []render.HelpRow{
		{Group: true, Cmd: "global"},
		{Bind: "ctrl+b", Cmd: "prefix mode"},
		{Bind: "prefix+?", Cmd: "keybinds"},
		{Bind: "prefix+s", Cmd: "settings"},
		{Bind: "prefix+q", Cmd: "detach"},
		{Bind: "prefix+shift+r", Cmd: "reload config"},
		{Bind: "prefix+o", Cmd: "open notification target"},
		{},
		{Group: true, Cmd: "navigation"},
		{Bind: "esc", Cmd: "back"},
		{Bind: "up / down", Cmd: "workspace list"},
		{Bind: "h / j / k / l / left / right", Cmd: "move focus"},
		{Bind: "tab / shift+tab", Cmd: "cycle pane"},
		{Bind: "enter", Cmd: "open workspace"},
		{Bind: "1..9", Cmd: "switch workspace"},
		{},
		{Group: true, Cmd: "workspaces / tabs"},
		{Bind: "prefix+w", Cmd: "workspace navigation"},
		{Bind: "prefix+g", Cmd: "session navigator"},
		{Bind: "prefix+shift+n", Cmd: "new workspace"},
		{Bind: "prefix+shift+g", Cmd: "new worktree"},
		{Bind: "unset", Cmd: "open worktree"},
		{Bind: "unset", Cmd: "delete worktree checkout"},
		{Bind: "prefix+shift+w", Cmd: "rename workspace"},
		{Bind: "prefix+shift+d", Cmd: "close workspace"},
		{Bind: "unset", Cmd: "previous workspace"},
		{Bind: "unset", Cmd: "next workspace"},
		{Bind: "unset", Cmd: "switch workspace 1-9"},
		{Bind: "unset", Cmd: "previous agent"},
		{Bind: "unset", Cmd: "next agent"},
		{Bind: "unset", Cmd: "focus agent 1-9"},
		{Bind: "prefix+c", Cmd: "new tab"},
		{Bind: "prefix+shift+t", Cmd: "rename tab"},
		{Bind: "prefix+p", Cmd: "previous tab"},
		{Bind: "prefix+n", Cmd: "next tab"},
		{Bind: "prefix+1..9", Cmd: "switch tab 1-9"},
		{Bind: "prefix+shift+x", Cmd: "close tab"},
		{},
		{Group: true, Cmd: "panes"},
		{Bind: bindKeys(a.splitVKeys, 'v'), Cmd: "split vertical"},
		{Bind: bindKeys(a.splitHKeys, '-'), Cmd: "split horizontal"},
		{Bind: "prefix+x", Cmd: "close pane"},
		{Bind: bindKeys(a.renameKeys, 'P'), Cmd: "rename pane"},
		{Bind: bindKeys(a.editKeys, 'e'), Cmd: "edit scrollback"},
		{Bind: bindKeys(a.copyKeys, '['), Cmd: "copy mode"},
		{Bind: "prefix+z", Cmd: "zoom pane"},
		{Bind: "prefix+r", Cmd: "resize mode"},
		{Bind: "prefix+b", Cmd: "toggle sidebar"},
		{Bind: "prefix+h", Cmd: "focus pane left"},
		{Bind: "prefix+j", Cmd: "focus pane down"},
		{Bind: "prefix+k", Cmd: "focus pane up"},
		{Bind: "prefix+l", Cmd: "focus pane right"},
		{Bind: "prefix+tab", Cmd: "cycle pane next"},
		{Bind: "prefix+shift+tab", Cmd: "cycle pane previous"},
		{Bind: bindKeys(a.historyBackKeys), Cmd: "focus history back"},
		{Bind: bindKeys(a.historyForwardKeys), Cmd: "focus history forward"},
		{Bind: "unset", Cmd: "last pane"},
		{},
	}
	for i := range rows {
		if rows[i].Cmd == "workspace list" || rows[i].Cmd == "move focus" {
			names := []string{"navigate_workspace_up", "navigate_workspace_down"}
			if rows[i].Cmd == "move focus" {
				names = []string{"navigate_pane_left", "navigate_pane_down", "navigate_pane_up", "navigate_pane_right"}
			}
			var bindings []string
			for _, name := range names {
				for _, spec := range a.sidebarConfig.ActionBindings(name) {
					if spec != "" {
						bindings = append(bindings, spec)
					}
				}
			}
			if rows[i].Cmd == "move focus" {
				bindings = append(bindings, "left", "right")
			}
			rows[i].Bind = strings.Join(bindings, " / ")
			if rows[i].Bind == "" {
				rows[i].Bind = "unset"
			}
			continue
		}
		if rows[i].Cmd == "prefix mode" {
			rows[i].Bind = a.prefixSpec()
			continue
		}

		name := strings.ReplaceAll(rows[i].Cmd, " ", "_")
		aliases := map[string]string{"keybinds": "help", "workspace navigation": "workspace_picker", "session navigator": "goto", "switch workspace 1-9": "switch_workspace", "switch tab 1-9": "switch_tab", "focus agent 1-9": "focus_agent", "zoom pane": "zoom", "delete worktree checkout": "remove_worktree"}
		if alias, ok := aliases[rows[i].Cmd]; ok {
			name = alias
		}
		if _, ok := config.ActionDefaults[name]; ok {
			bind := strings.Join(a.sidebarConfig.ActionBindings(name), " / ")
			if bind == "" {
				bind = "unset"
			}
			rows[i].Bind = bind
		}
	}
	if len(a.sidebarConfig.Keys.Command) > 0 {
		rows = append(rows, render.HelpRow{Group: true, Cmd: "custom commands"})
		for _, b := range a.sidebarConfig.Keys.Command {
			description := b.Description
			if description == "" {
				description = "custom command"
			}
			rows = append(rows, render.HelpRow{Bind: b.Key, Cmd: description})
		}
		rows = append(rows, render.HelpRow{})
	}
	return rows

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
	return max(1, render.HelpRect(a.st.Cols, a.st.Rows).H-7)
}

func (a *App) helpMaxScroll() int {
	r := render.HelpRect(a.st.Cols, a.st.Rows)
	if m := len(render.HelpLines(a.helpRows(), r.W-3, render.Palette{})) - a.helpBody(); m > 0 {
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
	case k.Code == uv.KeyEscape || k.Code == uv.KeyEnter || k.Code == '?':
		a.help = nil
	case k.Code == uv.KeyHome:
		a.helpScrollBy(-a.help.scroll)
	case k.Code == uv.KeyEnd:
		a.helpScrollBy(a.helpMaxScroll())
	case k.Code == uv.KeyUp || k.Code == 'k':
		a.helpScrollBy(-1)
	case k.Code == uv.KeyDown || k.Code == 'j':
		a.helpScrollBy(1)
	case k.Code == uv.KeyPgUp:
		a.helpScrollBy(-8)
	case k.Code == uv.KeyPgDown:
		a.helpScrollBy(8)
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
		top := m.Y - h.rect.Y - 4 - h.grab
		a.helpScrollBy(helpScrollFromThumb(body, maxScroll, top) - h.scroll)
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
		if render.HelpContentVisible(r) && m.Button == uv.MouseLeft && m.Y == r.Y+1 && m.X >= r.X+r.W-12 && m.X < r.X+r.W-1 {
			a.help = nil
			break
		}
		if render.HelpContentVisible(r) && m.Button == uv.MouseLeft && maxScroll > 0 && m.X == r.X+r.W-2 && m.Y >= r.Y+4 && m.Y < r.Y+4+body {
			th, top := render.HelpThumb(body, body+maxScroll, h.scroll)
			rel := m.Y - r.Y - 4
			h.grab = rel - top
			if rel < top || rel >= top+th {
				h.grab = th / 2
				a.helpScrollBy(helpScrollFromThumb(body, maxScroll, rel-h.grab) - h.scroll)
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

func helpScrollFromThumb(body, limit, top int) int {
	thumb, _ := render.HelpThumb(body, body+limit, 0)
	track := body - thumb
	if track <= 0 {
		return 0
	}
	return min(limit, max(0, int(math.Round(float64(top*limit)/float64(track)))))
}
