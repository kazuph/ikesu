package server

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/runtime"
)

func (s *Server) paneList(id string, p api.PaneListParams) api.Response {
	st := s.app.State()
	out := api.PaneListResult{Type: "pane_list", Panes: []api.PaneInfo{}}
	for i, ws := range st.Workspaces {
		if p.WorkspaceID != nil && *p.WorkspaceID != "" {
			if s.findWorkspace(*p.WorkspaceID) != i {
				continue
			}
		}
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if info, ok := s.paneInfo(pid); ok {
					out.Panes = append(out.Panes, info)
				}
			}
		}
	}
	return api.Success(id, out)
}

func (s *Server) paneGet(id, ref string) api.Response {
	pid, _, _, ok := s.findPane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	info, _ := s.paneInfo(pid)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

// paneCurrent resolves the calling pane: explicit caller_pane_id wins;
// otherwise we walk the caller process's ancestry looking for a pane shell.
func (s *Server) paneCurrent(id string, p api.PaneCurrentParams) api.Response {
	if p.CallerPaneID != nil && *p.CallerPaneID != "" {
		pid, _, _, ok := s.findPane(*p.CallerPaneID)
		if !ok {
			return s.paneNotFound(id, *p.CallerPaneID)
		}
		// When the caller process is known, the asserted pane must be
		// provable from its ancestry — a leaked GTX_PANE_ID in a process
		// outside the pane fails closed (SPEC G5).
		if p.CallerProcessID != nil {
			if proven := s.paneForProcess(int(*p.CallerProcessID)); proven != pid {
				return api.Failure(id, api.ErrPaneNotFound,
					fmt.Sprintf("caller pane %s could not be proven from process ancestry", *p.CallerPaneID))
			}
		}
		info, _ := s.paneInfo(pid)
		return api.Success(id, api.PaneCurrentResult{Type: "pane_current", Pane: info})
	}
	if p.CallerProcessID != nil {
		if pid := s.paneForProcess(int(*p.CallerProcessID)); pid > 0 {
			info, _ := s.paneInfo(pid)
			return api.Success(id, api.PaneCurrentResult{Type: "pane_current", Pane: info})
		}
	}
	return api.Failure(id, api.ErrPaneNotFound, "caller is not inside a gtx pane")
}

// paneForProcess walks the ancestry of pid looking for a pane shell.
func (s *Server) paneForProcess(pid int) int {
	st := s.app.State()
	shells := map[int]int{} // shell pid → pane id
	for _, ws := range st.Workspaces {
		for _, t := range ws.Tabs {
			for _, id := range t.Layout.PaneIDs() {
				if p := s.app.Supervisor().Get(id); p != nil {
					if sp := p.ShellPID(); sp > 0 {
						shells[sp] = id
					}
				}
			}
		}
	}
	for cur, depth := pid, 0; cur > 1 && depth < 64; depth++ {
		if pane, ok := shells[cur]; ok {
			return pane
		}
		next := platform.PPID(cur)
		if next <= 0 || next == cur {
			break
		}
		cur = next
	}
	return 0
}

func (s *Server) paneFocus(id, ref string) api.Response {
	pid, wsIdx, _, ok := s.findPane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	st := s.app.State()
	if wsIdx != st.Active {
		s.app.ExecCmd(model.CmdSelectWorkspace{Index: wsIdx})
	}
	s.app.ExecCmd(model.CmdFocusPane{Pane: pid})
	info, _ := s.paneInfo(pid)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

func (s *Server) paneRename(id string, p api.PaneRenameParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	label := ""
	if p.Label != nil {
		label = *p.Label
	}
	s.app.ExecCmd(model.CmdRenamePane{Pane: pid, Label: label})
	info, _ := s.paneInfo(pid)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

func (s *Server) paneRead(id string, p api.PaneReadParams) api.Response {
	pid, wsIdx, tabIdx, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	source := p.Source
	if source == "" {
		source = "recent"
	}
	format := p.Format
	if format == "" {
		format = "text"
	}
	if format != "text" {
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("read format %q is not supported in this stage", format))
	}
	limit := 80
	if p.Lines != nil {
		limit = int(*p.Lines)
		if limit > 1000 {
			limit = 1000
		}
	}
	screen := pane.ScreenLines
	scroll := pane.ScrollbackLines
	var lines []string
	var truncated bool
	switch source {
	case "visible":
		lines = screen()
	case "recent":
		lines = append(scroll(), screen()...)
	case "recent_unwrapped", "recent-unwrapped", "detection":
		// The vt emulator does not record wrap flags on scrollback rows,
		// and gtx detection is process/OSC based, not buffer based; both
		// sources exist in the herdr schema but are unimplemented here.
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("read source %q is not supported in this stage", source))
	default:
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("unknown read source %q", source))
	}
	// Trailing blank rows carry no information (herdr trims them the same
	// way), so a mostly-empty screen reads as its content, not blank space.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
		truncated = true
	}
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	return api.Success(id, api.PaneReadResultEnvelope{
		Type: "pane_read",
		Read: api.PaneReadResult{
			PaneID:      fmtPaneID(pid),
			WorkspaceID: fmtWorkspaceID(ws.ID),
			TabID:       fmtTabID(ws.ID, ws.Tabs[tabIdx].ID),
			Source:      source,
			Format:      format,
			Text:        strings.Join(lines, "\n"),
			Revision:    pane.Snapshot().Revision,
			Truncated:   truncated,
		},
	})
}

func (s *Server) paneSplit(id string, p api.PaneSplitParams) api.Response {
	st := s.app.State()
	var dir layout.Direction
	switch p.Direction {
	case "right", "vertical":
		dir = layout.Vertical
	case "down", "horizontal":
		dir = layout.Horizontal
	default:
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid split direction %q", p.Direction))
	}
	target := st.FocusedPane()
	targetTab := 0
	wsIdx := st.Active
	if p.WorkspaceID != nil && *p.WorkspaceID != "" {
		wsIdx = s.findWorkspace(*p.WorkspaceID)
		if wsIdx < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
		ws := st.Workspaces[wsIdx]
		targetTab = ws.ActiveTab
		target = ws.ActiveLayout().Focused()
	}
	if p.TargetPaneID != nil && *p.TargetPaneID != "" {
		pid, idx, ti, ok := s.findPane(*p.TargetPaneID)
		if !ok {
			return s.paneNotFound(id, *p.TargetPaneID)
		}
		target, wsIdx, targetTab = pid, idx, ti
	}
	ratio := 0.5
	if p.Ratio != nil {
		ratio = *p.Ratio
	}
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if cwd == "" {
		if pane := s.app.Supervisor().Get(target); pane != nil {
			cwd = pane.SpawnCwd()
		}
	}
	s.app.ExecCmd(model.CmdSplitPane{
		Workspace: wsIdx, Pane: target, Dir: dir,
		Ratio: ratio, Focus: p.Focus, Cwd: cwd, Env: p.Env,
	})
	// New pane is the one just allocated, inserted right after target in
	// the target's own tab (PaneIDs keeps creation order).
	pids := st.Workspaces[wsIdx].Tabs[targetTab].Layout.PaneIDs()
	if len(pids) == 0 {
		return api.Failure(id, api.ErrInvalidRequest, "split failed")
	}
	newID := pids[len(pids)-1]
	for i, v := range pids {
		if v == target && i+1 < len(pids) {
			newID = pids[i+1]
		}
	}
	info, _ := s.paneInfo(newID)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

func (s *Server) paneClose(id, ref string) api.Response {
	pid, _, _, ok := s.findPane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdClosePane{Pane: pid})
	return api.Success(id, api.OkResult{Type: "ok"})
}

func (s *Server) paneSendText(id string, p api.PaneSendTextParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	// Literal text; bracketed-paste markers when the pane asked for them.
	pane.WithEmu(func(e *vt.Emulator) { e.Paste(p.Text) })
	return api.Success(id, api.OkResult{Type: "ok"})
}

func (s *Server) paneSendKeys(id string, p api.PaneSendKeysParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	keys, bad := parseKeys(p.Keys)
	if bad != "" {
		return api.Failure(id, api.ErrInvalidKey, fmt.Sprintf("unsupported key %s", bad))
	}
	pane.WithEmu(func(e *vt.Emulator) { e.SendKeys(keys...) })
	return api.Success(id, api.OkResult{Type: "ok"})
}

func (s *Server) paneSendInput(id string, p api.PaneSendInputParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, p.PaneID)
	}
	keys, bad := parseKeys(p.Keys)
	if bad != "" {
		return api.Failure(id, api.ErrInvalidKey, fmt.Sprintf("unsupported key %s", bad))
	}
	s.sendInputToPane(pane, p.Text, keys)
	return api.Success(id, api.OkResult{Type: "ok"})
}

// sendInputToPane writes text then, after the required 500 ms settle
// window, sends the keys — the agent CLIs rely on the gap so TUI apps
// have consumed the prompt before Enter arrives. Runs asynchronously so
// the app loop never blocks.
func (s *Server) sendInputToPane(pane *runtime.Pane, text string, keys []uv.KeyEvent) {
	go func() {
		if text != "" {
			pane.WithEmu(func(e *vt.Emulator) { e.Paste(text) })
		}
		if len(keys) == 0 {
			return
		}
		if text != "" {
			time.Sleep(500 * time.Millisecond)
		}
		pane.WithEmu(func(e *vt.Emulator) { e.SendKeys(keys...) })
	}()
}

func (s *Server) paneReportAgent(id string, p api.PaneReportAgentParams) api.Response {
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	if pane := s.app.Supervisor().Get(pid); pane != nil {
		pane.SetAgent(p.Agent)
	}
	s.app.SetReported(pid, p.Agent, p.State)
	if p.Title != nil && *p.Title != "" {
		// Terminal title override is tracked via the reported record only;
		// the OSC title stays the pane's own.
	}
	return api.Success(id, api.OkResult{Type: "ok"})
}

// --- pane info methods ---

// resolvePane defaults an empty pane ref to the focused pane.
func (s *Server) resolvePane(ref string) (pid, wsIdx, tabIdx int, ok bool) {
	if ref == "" {
		st := s.app.State()
		ws := st.ActiveWorkspace()
		t := ws.TabRef()
		if ws == nil || t == nil {
			return 0, -1, -1, false
		}
		return t.Layout.Focused(), st.Active, ws.ActiveTab, true
	}
	return s.findPane(ref)
}
