package tui

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

type navigatorView struct {
	query            string
	searching        bool
	filter           string
	selected, scroll int
	collapsed        map[int]bool
}

func (a *App) openNavigator() {
	a.navigator = &navigatorView{}
	rows, _ := a.navigatorRows()
	for i, row := range rows {
		if row.Pane == a.focusedPane() {
			a.navigator.selected = i
			break
		}
	}
	a.navigatorMove(0)
	a.workspaceNavigate = false
	a.dirty = true
}

func (a *App) navigatorRows() ([]render.NavigatorRow, int) {
	n := a.navigator
	var rows []render.NavigatorRow
	count := 0
	for wi, ws := range a.st.Workspaces {
		name := ws.Name
		if display := a.workspaceNames[wi]; display != "" {
			name = display
		}
		var children []render.NavigatorRow
		for ti, tab := range ws.Tabs {
			var tabRows []render.NavigatorRow
			tabName := ""
			depth := 1
			if len(ws.Tabs) > 1 {
				tabName = tab.Name
				if tabName == "" {
					tabName = fmt.Sprint(ti + 1)
				}
				depth = 2
			}
			for _, pid := range tab.Layout.PaneIDs() {
				count++
				glyph := a.glyph(pid)
				if n.filter != "" && glyph.Key != n.filter {
					continue
				}
				title := a.st.PaneLabels[pid]
				if title == "" {
					title = fmt.Sprintf("pane %d", pid)
				}
				agent := "shell"
				if p := a.sup.Get(pid); p != nil {
					if id := p.Snapshot().AgentID; id != "" {
						agent = id
					}
				}
				if n.query != "" && !strings.Contains(strings.ToLower(title+" "+agent), strings.ToLower(n.query)) {
					continue
				}
				status := glyph.Rune
				if glyph.Rune == "·" {
					status = "○"
				}
				tabRows = append(tabRows, render.NavigatorRow{Workspace: wi, Tab: ti, Pane: pid, Depth: depth, TabName: tabName, Name: title, WorkspaceName: name, Agent: agent, Status: status, Current: pid == a.focusedPane()})
			}

			if len(tabRows) > 0 {
				if tabName != "" {
					children = append(children, render.NavigatorRow{Workspace: wi, Tab: ti, Pane: -2, Depth: 1, Name: tabName, TabName: tabName, WorkspaceName: name, Agent: fmt.Sprintf("%d panes", tab.Layout.PaneCount()), Count: tab.Layout.PaneCount(), Status: "○"})
				}
				children = append(children, tabRows...)
			}
		}
		workspaceMatch := n.query != "" && n.filter == "" && strings.Contains(strings.ToLower(name), strings.ToLower(n.query))
		if len(children) == 0 && !workspaceMatch {
			continue
		}
		rows = append(rows, render.NavigatorRow{Workspace: wi, Pane: -1, Name: name, WorkspaceName: name, Count: ws.PaneCount(), Status: "○", Current: wi == a.st.Active})
		if !n.collapsed[wi] || n.query != "" || n.filter != "" {
			rows = append(rows, children...)
		}
	}
	return rows, count
}

func (a *App) navigatorMove(delta int) {
	rows, _ := a.navigatorRows()
	a.navigator.selected = max(0, min(max(0, len(rows)-1), a.navigator.selected+delta))
	visible := max(1, render.NavigatorRect(a.st.Cols, a.st.Rows).H-6)
	if a.navigator.selected < a.navigator.scroll {
		a.navigator.scroll = a.navigator.selected
	}
	if a.navigator.selected >= a.navigator.scroll+visible {
		a.navigator.scroll = a.navigator.selected - visible + 1
	}
	a.dirty = true
}

func (a *App) navigatorSelect() {
	rows, _ := a.navigatorRows()
	n := a.navigator
	if len(rows) == 0 {
		return
	}
	row := rows[min(n.selected, len(rows)-1)]
	if row.Pane == -1 {
		a.exec(model.CmdSelectWorkspace{Index: row.Workspace})
	} else {
		a.exec(model.CmdSelectTab{Workspace: row.Workspace, Tab: row.Tab})
		if row.Pane >= 0 {
			a.exec(model.CmdFocusPane{Pane: row.Pane})
		}
	}
	a.navigator = nil
	a.dirty = true
}

func (a *App) navigatorKey(k uv.Key) {
	n := a.navigator
	if k.Code == uv.KeyEscape {
		if n.searching {
			n.searching = false
		} else {
			a.navigator = nil
		}
		a.dirty = true
		return
	}
	if k.Mod == uv.ModCtrl {
		switch k.Code {
		case 'n':
			if n.searching {
				a.navigatorMove(1)
				return
			}
		case 'p':
			if n.searching {
				a.navigatorMove(-1)
				return
			}
		case 'd', 'u':
			if !n.searching {
				delta := max(1, (render.NavigatorRect(a.st.Cols, a.st.Rows).H-6)/2)
				if k.Code == 'u' {
					delta = -delta
				}
				a.navigatorMove(delta)
				return
			}
		}
	}
	switch k.Code {
	case uv.KeyUp:
		a.navigatorMove(-1)
		return
	case uv.KeyDown:
		a.navigatorMove(1)
		return
	case uv.KeyEnter:
		a.navigatorSelect()
		return
	case uv.KeyHome, uv.KeyEnd:
		if n.searching {
			return
		}
		rows, _ := a.navigatorRows()
		if k.Code == uv.KeyHome {
			n.selected = 0
		} else {
			n.selected = max(0, len(rows)-1)
		}
		a.navigatorMove(0)
		return
	}
	if n.searching {
		switch {
		case k.Code == 'u' && k.Mod == uv.ModCtrl:
			n.query = ""
		case k.Code == uv.KeyBackspace:
			runes := []rune(n.query)
			if len(runes) > 0 {
				n.query = string(runes[:len(runes)-1])
			}
		case k.Text != "" && !k.Mod.Contains(uv.ModCtrl):
			n.query += k.Text
		}
	} else {
		if k.Text == "G" || k.Code == 'G' || k.Code == 'g' && k.Mod == uv.ModShift {
			rows, _ := a.navigatorRows()
			n.selected = max(0, len(rows)-1)
			a.navigatorMove(0)
			return
		}
		if k.Mod != 0 {
			return
		}
		switch k.Code {
		case '/':
			n.searching = true
			n.filter = ""
		case 'j':
			a.navigatorMove(1)
		case 'k':
			a.navigatorMove(-1)
		case ' ':
			rows, _ := a.navigatorRows()
			if len(rows) > 0 && rows[min(n.selected, len(rows)-1)].Pane == -1 {
				row := rows[min(n.selected, len(rows)-1)]
				if n.collapsed == nil {
					n.collapsed = make(map[int]bool)
				}
				n.collapsed[row.Workspace] = !n.collapsed[row.Workspace]
			}
		case 'b':
			n.query = ""
			n.filter = "blocked"
		case 'w':
			n.query = ""
			n.filter = "working"
		case 'i':
			n.query = ""
			n.filter = "idle"
		case 'd':
			n.query = ""
			n.filter = "done"
		case 'a':
			n.query = ""
			n.filter = ""
		case uv.KeyBackspace:
			n.filter = ""
		}
	}
	a.navigatorMove(0)
	a.dirty = true
}

func (a *App) navigatorMouse(ev input.Event) {
	n := a.navigator
	switch ev.Kind {
	case input.KindMouseWheel:
		delta := 3
		if ev.Mouse.Button == uv.MouseWheelUp {
			delta = -3
		}
		rows, _ := a.navigatorRows()
		visible := max(1, render.NavigatorRect(a.st.Cols, a.st.Rows).H-6)
		n.scroll = max(0, min(max(0, len(rows)-visible), n.scroll+delta))
		n.selected = n.scroll
	case input.KindMouseClick:
		r := render.NavigatorRect(a.st.Cols, a.st.Rows)
		if ev.Mouse.Button != uv.MouseLeft {
			return
		}
		if !pointIn(ev.Mouse.X, ev.Mouse.Y, r) {
			a.navigator = nil
			break
		}
		index := ev.Mouse.Y - (r.Y + 3) + n.scroll
		rows, _ := a.navigatorRows()
		if ev.Mouse.Y < r.Y+3 || ev.Mouse.Y >= r.Y+r.H-3 || index < 0 || index >= len(rows) {
			return
		}
		n.selected = index
		a.navigatorSelect()
	}
	a.dirty = true
}

func (a *App) navigatorRender() *render.Navigator {
	if a.navigator == nil {
		return nil
	}
	n := a.navigator
	rows, count := a.navigatorRows()
	n.selected = max(0, min(n.selected, max(0, len(rows)-1)))
	visible := max(1, render.NavigatorRect(a.st.Cols, a.st.Rows).H-6)
	n.scroll = max(0, min(n.scroll, max(0, len(rows)-visible)))
	return &render.Navigator{Rows: rows, Total: count, Selected: n.selected, Scroll: n.scroll, Query: n.query, Searching: n.searching, Filter: n.filter}
}
