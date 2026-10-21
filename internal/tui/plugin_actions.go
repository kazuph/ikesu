package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/plugin"
	"regexp"
	"strings"
)

func (a *App) SetPluginHost(engine func() *plugin.Engine, context func() plugin.Context) {
	a.pluginHost = engine
	a.pluginContext = context
}
func (a *App) invokePluginAction(id string) {
	if a.pluginHost == nil {
		a.ShowToast("plugin action", "unsupported: plugin host is unavailable", "error", -1)
		return
	}
	context := a.actionContext("keybind")
	if _, _, err := a.pluginHost().Invoke("", id, context); err != nil {
		a.ShowToast("plugin action", err.Error(), "error", -1)
	}
}
func (a *App) actionContext(source string) plugin.Context {
	context := plugin.Context{}
	if a.pluginContext != nil {
		context = a.pluginContext()
	}
	context["invocation_source"] = source
	if ws := a.st.ActiveWorkspace(); ws != nil {
		context["workspace_id"] = fmt.Sprintf("s%d", ws.ID)
		context["workspace_label"] = ws.Name
		context["workspace_cwd"] = ws.Cwd
		if tab := ws.TabRef(); tab != nil {
			context["tab_id"] = fmt.Sprintf("s%d:t%d", ws.ID, tab.ID)
			context["tab_label"] = tab.Name
		}
	}
	pid := a.st.FocusedPane()
	if a.st.OverlayVisible() {
		pid = a.st.Overlay.PaneID
	}
	if p := a.sup.Get(pid); p != nil {
		sn := p.Snapshot()
		context["focused_pane_id"] = fmt.Sprintf("p%d", pid)
		context["focused_pane_cwd"] = sn.Cwd
		context["focused_pane_status"] = a.glyph(pid).Key
		if sn.AgentID != "" {
			context["focused_pane_agent"] = sn.AgentID
		}
	}
	if a.sel != nil {
		if text, n := a.selText(a.sel); n > 0 {
			context["selected_text"] = text
		}
	}
	return context
}

var terminalURL = regexp.MustCompile(`https?://[^\s<>"']+`)

func (a *App) pluginLinkClick(pid int, m uv.Mouse) bool {
	if a.pluginHost == nil || m.Button != uv.MouseLeft || !m.Mod.Contains(uv.ModCtrl) {
		return false
	}
	r := a.innerRect(pid)
	if !pointIn(m.X, m.Y, r) {
		return false
	}
	p := a.sup.Get(pid)
	if p == nil {
		return false
	}
	x, y := m.X-r.X, m.Y-r.Y
	lines := p.BufferLines()
	row := viewTop(p, a.scroll[pid], r.H) + y
	if row < 0 || row >= len(lines) || x < 0 || x >= len(lines[row]) {
		return false
	}
	url := lines[row][x].Link.URL
	if url == "" {
		var text strings.Builder
		for _, cell := range lines[row] {
			text.WriteString(cell.Content)
		}
		line := text.String()
		for _, span := range terminalURL.FindAllStringIndex(line, -1) {
			left := ansi.StringWidth(line[:span[0]])
			right := left + ansi.StringWidth(line[span[0]:span[1]])
			if x >= left && x < right {
				url = line[span[0]:span[1]]
				break
			}
		}
	}
	if url == "" {
		return false
	}
	found, err := a.pluginHost().InvokeLink(url, a.actionContext("link_click"))
	if err != nil {
		a.ShowToast("plugin link", err.Error(), "error", -1)
	}
	return found
}
