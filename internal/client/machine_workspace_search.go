package client

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

const workspaceSearchMaxRows = 10

// workspaceSearch is the client's "go to space" overlay. It lists the spaces of
// every connected machine in the order of the cycle keys and owns no server state.
type workspaceSearch struct {
	query []rune
	sel   int
	top   int
}

type workspaceSearchItem struct {
	target                workspaceCycleTarget
	name, machine, branch string
	hq                    bool
}

// workspaceSearchButtons are the tappable 🔍 cells recorded by the last paint.
type workspaceSearchButtons struct {
	sidebar, mobile layout.Rect
}

func (m *machineClient) workspaceSearchItems() []workspaceSearchItem {
	hq := m.oyagoiForSidebar()
	var items []workspaceSearchItem
	for _, target := range m.workspaceCycleTargets() {
		e := m.endpoints[target.machine]
		if e == nil || e.snapshot == nil || e.snapshot.State == nil {
			continue
		}
		in := e.snapshot
		for i, ws := range in.State.Workspaces {
			if ws.ID != target.workspace {
				continue
			}
			name := in.WorkspaceNames[i]
			if name == "" || ws.CustomName {
				name = render.WorkspaceDisplayName(ws, in.Panes)
			}
			label := e.profile.Label
			if label == "" {
				label = target.machine
			}
			items = append(items, workspaceSearchItem{target: target, name: name, machine: label, branch: in.Git[i].Branch, hq: target.generation != "" && hq.Machine == target.machine && hq.Workspace == target.workspace})
			break
		}
	}
	return items
}

// filterWorkspaceSearch keeps the cycle order. Every space-separated word of the
// query must be a case-insensitive substring of the name, machine or branch.
func filterWorkspaceSearch(items []workspaceSearchItem, query []rune) []workspaceSearchItem {
	tokens := strings.Fields(strings.ToLower(string(query)))
	if len(tokens) == 0 {
		return items
	}
	var out []workspaceSearchItem
	for _, item := range items {
		text := strings.ToLower(item.name + " " + item.machine + " " + item.branch)
		match := true
		for _, token := range tokens {
			match = match && strings.Contains(text, token)
		}
		if match {
			out = append(out, item)
		}
	}
	return out
}

func (m *machineClient) workspaceSearchResults() []workspaceSearchItem {
	if m.search == nil {
		return nil
	}
	results := filterWorkspaceSearch(m.workspaceSearchItems(), m.search.query)
	m.search.sel = max(0, min(m.search.sel, len(results)-1))
	return results
}

type workspaceSearchGeometry struct {
	box        layout.Rect
	list       layout.Rect
	listHeight int
}

func (m *machineClient) workspaceSearchGeometry(count int) workspaceSearchGeometry {
	cols, rows := m.size.Cols, m.size.Rows
	w := max(1, min(cols, min(72, cols-4)))
	listH := max(1, min(count, min(workspaceSearchMaxRows, rows-4)))
	h := min(rows, 2+listH)
	x, y := (cols-w)/2, min(1, max(0, rows-h))
	return workspaceSearchGeometry{box: layout.Rect{X: x, Y: y, W: w, H: h}, list: layout.Rect{X: x, Y: y + 2, W: w, H: max(0, h-2)}, listHeight: max(0, h-2)}
}

func (m *machineClient) openWorkspaceSearch() {
	if m.search == nil {
		m.search = &workspaceSearch{}
	}
}

// workspaceSearchKey reports whether the key opens the overlay. A prefix chord
// needs the server's pending prefix; plain chords must not be inside one.
func (m *machineClient) workspaceSearchKey(event input.Event) (open, prefixed bool) {
	if event.Kind != input.KindKey {
		return false, false
	}
	local, active := m.endpoints["local"], m.endpoints[m.active]
	if active == nil || active.snapshot == nil || active.snapshot.State == nil || active.connection == nil {
		return false, false
	}
	cfg := active.snapshot.SidebarConfig
	if local != nil && local.snapshot != nil {
		cfg = local.snapshot.SidebarConfig
	}
	view := *active.snapshot
	if m.forms != nil {
		m.forms.Overlay(&view)
	}
	if view.Dialog != nil || view.Menu != nil || view.Help != nil || view.Navigator != nil || view.Settings != nil || view.Copy != nil || view.ModeHint != "" && !view.Prefix {
		return false, false
	}
	if workspaceCycleCommand(active.snapshot.SidebarConfig, event.Key) {
		return false, false
	}
	bindings := cfg.ActionBindings("workspace_search")
	if _, configured := cfg.Keys.Actions["workspace_search"]; configured && len(bindings) > 0 && bindings[0] != "" {
		// A configured key adds to prefix+/ rather than replacing it.
		bindings = append(bindings[:len(bindings):len(bindings)], config.ActionDefaults["workspace_search"]...)
	}
	for _, binding := range bindings {
		chord, err := config.ParseChord(binding)
		if err == nil && chord.Match(event.Key, view.Prefix) {
			return true, view.Prefix
		}
	}
	return false, false
}

// workspaceSearchInput consumes the events the overlay owns (the caller then
// repaints); false lets the event continue to the normal input path.
func (m *machineClient) workspaceSearchInput(event input.Event) (handled bool) {
	if m.search == nil {
		return m.workspaceSearchOpenInput(event)
	}
	switch event.Kind {
	case input.KindKeyRelease, input.KindMouseRelease, input.KindMouseMotion:
		return true
	case input.KindPaste:
		m.search.query = append(m.search.query, []rune(strings.NewReplacer("\r", "", "\n", " ").Replace(event.Text))...)
		m.search.sel, m.search.top = 0, 0
		return true
	case input.KindKey:
		m.workspaceSearchKeyPress(event.Key)
		return true
	case input.KindMouseWheel:
		if event.Mouse.Button == uv.MouseWheelUp {
			m.workspaceSearchMove(-1)
		} else if event.Mouse.Button == uv.MouseWheelDown {
			m.workspaceSearchMove(1)
		}
		return true
	case input.KindMouseClick:
		m.workspaceSearchClick(m.mouse.CellEvent(event, m.size).Mouse)
		return true
	}
	return false
}

func (m *machineClient) workspaceSearchOpenInput(event input.Event) bool {
	switch event.Kind {
	case input.KindKey:
		open, prefixed := m.workspaceSearchKey(event)
		if !open {
			return false
		}
		active := m.endpoints[m.active]
		if prefixed {
			// The overlay replaces the key after the prefix, so end the server's wait here.
			if active.connection.writer.available >= endpoint.NotificationGeneration {
				if err := active.connection.enqueue(msg{Type: "input", EndPrefix: true}); err != nil {
					m.notify(err.Error())
					return true
				}
			} else if err := m.sendInput(m.active, eventBytes(event)); err != nil {
				m.notify(err.Error())
				return true
			}
		}
		m.consumeWorkspaceKey(event.Key.Code)
		m.openWorkspaceSearch()
		return true
	case input.KindMouseClick:
		if event.Mouse.Button != uv.MouseLeft {
			return false
		}
		cell := m.mouse.CellEvent(event, m.size).Mouse
		for _, r := range []layout.Rect{m.searchButtons.sidebar, m.searchButtons.mobile} {
			if r.W > 0 && contains(r.X, r.Y, r.W, r.H, cell.X, cell.Y) {
				m.openWorkspaceSearch()
				return true
			}
		}
	case input.KindMouseRelease:
		// The press that opened the overlay may release after it closed.
		if m.searchRelease {
			m.searchRelease = false
			return true
		}
	}
	return false
}

func (m *machineClient) workspaceSearchKeyPress(key uv.Key) {
	s := m.search
	switch {
	case key.Code == uv.KeyEscape && key.Mod == 0:
		m.search = nil
	case key.Code == uv.KeyEnter && !key.Mod.Contains(uv.ModCtrl):
		results := m.workspaceSearchResults()
		if len(results) == 0 {
			return
		}
		m.chooseWorkspaceSearch(results[s.sel])
		m.consumeWorkspaceKey(key.Code)
	case key.Code == uv.KeyUp && key.Mod == 0, key.Code == 'p' && key.Mod == uv.ModCtrl:
		m.workspaceSearchMove(-1)
	case key.Code == uv.KeyDown && key.Mod == 0, key.Code == 'n' && key.Mod == uv.ModCtrl:
		m.workspaceSearchMove(1)
	case key.Code == uv.KeyBackspace && key.Mod == 0:
		if len(s.query) > 0 {
			s.query = s.query[:len(s.query)-1]
			s.sel, s.top = 0, 0
		}
	case key.Code == 'u' && key.Mod == uv.ModCtrl:
		s.query, s.sel, s.top = nil, 0, 0
	case key.Mod&(uv.ModCtrl|uv.ModAlt|uv.ModSuper|uv.ModHyper|uv.ModMeta) == 0:
		text := key.Text
		if text == "" && unicode.IsPrint(key.Code) {
			text = string(key.Code)
		}
		if text != "" {
			s.query = append(s.query, []rune(text)...)
			s.sel, s.top = 0, 0
		}
	}
	if m.search != nil {
		m.workspaceSearchResults()
		m.workspaceSearchScroll()
	}
}

func (m *machineClient) workspaceSearchMove(delta int) {
	results := m.workspaceSearchResults()
	m.search.sel = max(0, min(max(0, len(results)-1), m.search.sel+delta))
	m.workspaceSearchScroll()
}

// workspaceSearchScroll keeps the selected row inside the visible window.
func (m *machineClient) workspaceSearchScroll() {
	count := len(m.workspaceSearchResults())
	g := m.workspaceSearchGeometry(count)
	s := m.search
	if g.listHeight == 0 {
		return
	}
	s.top = max(0, min(s.top, max(0, count-g.listHeight)))
	if s.sel < s.top {
		s.top = s.sel
	}
	if s.sel >= s.top+g.listHeight {
		s.top = s.sel - g.listHeight + 1
	}
}

func (m *machineClient) workspaceSearchClick(cell uv.Mouse) {
	results := m.workspaceSearchResults()
	g := m.workspaceSearchGeometry(len(results))
	if cell.Button != uv.MouseLeft {
		return
	}
	// A press that closes the overlay is followed by a release that must not reach a pane.
	defer func() { m.searchRelease = m.search == nil }()
	if !contains(g.box.X, g.box.Y, g.box.W, g.box.H, cell.X, cell.Y) {
		m.search = nil
		return
	}
	if len(results) > 0 && contains(g.list.X, g.list.Y, g.list.W, g.list.H, cell.X, cell.Y) {
		if index := m.search.top + cell.Y - g.list.Y; index < len(results) {
			m.chooseWorkspaceSearch(results[index])
		}
	}
}

// chooseWorkspaceSearch moves to the item exactly as the cycle keys do.
func (m *machineClient) chooseWorkspaceSearch(item workspaceSearchItem) {
	m.search = nil
	target := item.target
	e := m.endpoints[target.machine]
	if e == nil || e.connection == nil || e.snapshot == nil || e.snapshot.State == nil {
		return
	}
	if m.pending == nil && target.machine == m.active && target.generation == "" {
		if ws := e.snapshot.State.ActiveWorkspace(); ws != nil && ws.ID == target.workspace {
			return
		}
	}
	pending := &pendingMachine{id: target.machine, workspace: target.workspace, generation: target.generation, deadline: time.Now().Add(machineSwitchWait)}
	if m.pending != nil {
		pending.deadline = m.pending.deadline
		pending.keys = m.pending.keys
	}
	m.pending = pending
	m.requestPending()
}

// drawWorkspaceSearchUI paints the tappable 🔍 cells and the overlay.
func (m *machineClient) drawWorkspaceSearchUI(out *render.Output, in render.Input) {
	f := out.Frame
	m.searchButtons = workspaceSearchButtons{}
	s := in.State
	if s != nil && s.Mobile() && in.MobileSwitcher {
		r := layout.Rect{X: 0, Y: 2, W: s.Cols, H: 1}
		if s.Rows > 3 && r.W > 0 {
			style := uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}
			f.Fill(r.X, r.Y, r.W, 1, " ", uv.Style{Bg: f.Palette.PopupBG})
			f.Text(1, r.Y, " 🔍 search", style, s.Cols)
			m.searchButtons.mobile = r
		}
	} else if s != nil && !s.Mobile() && s.SidebarVisible && s.SidebarArea().W >= model.SidebarMinDefault {
		area := s.SidebarArea()
		for _, pos := range render.SidebarViewForInput(in).Rows {
			if pos.Right || pos.Row.Kind != render.RowTitle {
				continue
			}
			x := area.X + 8
			if x+2 <= area.X+area.W-1-len(render.WorkspaceDensityLabel(s.WorkspaceDensity)) {
				style := uv.Style{Fg: f.Palette.Text}
				if cell := f.Cell(x, pos.Y); cell != nil {
					style.Bg = cell.Style.Bg
				}
				f.Text(x, pos.Y, "🔍", style, x+2)
				m.searchButtons.sidebar = layout.Rect{X: x, Y: pos.Y, W: 2, H: 1}
			}
			break
		}
	}
	if m.search == nil {
		return
	}
	results := m.workspaceSearchResults()
	m.workspaceSearchScroll()
	g := m.workspaceSearchGeometry(len(results))
	box := g.box
	base := uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG}
	f.Fill(box.X, box.Y, box.W, box.H, " ", base)
	prompt := " 🔍 "
	query := string(m.search.query)
	f.Text(box.X, box.Y, prompt, uv.Style{Fg: f.Palette.Accent, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, box.X+box.W)
	qx := box.X + ansi.StringWidth(prompt)
	end := f.Text(qx, box.Y, ansi.Truncate(query, max(0, box.W-ansi.StringWidth(prompt)-8), ""), uv.Style{Fg: f.Palette.Text, Bg: f.Palette.PopupBG, Attrs: uv.AttrBold}, box.X+box.W)
	count := " " + strconv.Itoa(min(len(results), m.search.sel+1)) + "/" + strconv.Itoa(len(results)) + " "
	if cx := box.X + box.W - ansi.StringWidth(count); cx > end {
		f.Text(cx, box.Y, count, uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, box.X+box.W)
	}
	if box.H > 1 {
		f.Text(box.X, box.Y+1, strings.Repeat("─", box.W), uv.Style{Fg: f.Palette.PanelBG, Bg: f.Palette.PopupBG}, box.X+box.W)
	}
	if len(results) == 0 {
		if g.list.H > 0 {
			f.Text(g.list.X+2, g.list.Y, "該当なし", uv.Style{Fg: f.Palette.Dim, Bg: f.Palette.PopupBG}, box.X+box.W)
		}
	}
	for row := 0; row < g.list.H && m.search.top+row < len(results); row++ {
		index, y := m.search.top+row, g.list.Y+row
		item := results[index]
		bg, marker := f.Palette.PopupBG, " "
		if index == m.search.sel {
			bg, marker = f.Palette.Surface, "▌"
		}
		f.Fill(g.list.X, y, g.list.W, 1, " ", uv.Style{Bg: bg})
		f.Text(g.list.X, y, marker, uv.Style{Fg: f.Palette.Accent, Bg: bg}, g.list.X+1)
		name := item.name
		if item.hq {
			name = "🎏 " + name
		}
		limit := box.X + box.W
		x := f.Text(g.list.X+2, y, ansi.Truncate(name, max(0, g.list.W-3), "…"), uv.Style{Fg: f.Palette.Text, Bg: bg, Attrs: uv.AttrBold}, limit)
		detail := item.machine
		if item.branch != "" {
			detail += " · " + item.branch
		}
		if x+2 < limit {
			f.Text(x+2, y, ansi.Truncate(detail, max(0, limit-x-3), "…"), uv.Style{Fg: f.Palette.Dim, Bg: bg}, limit)
		}
	}
	out.AnchorX, out.AnchorY = min(box.X+box.W-1, qx+ansi.StringWidth(query)), box.Y
	out.AnchorVisible, out.AnchorShape = true, 0
}
