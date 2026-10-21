package server

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
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
	if st.Overlay != nil {
		if info, ok := s.paneInfo(st.Overlay.PaneID); ok && (p.WorkspaceID == nil || *p.WorkspaceID == "" || s.findWorkspace(*p.WorkspaceID) == st.WorkspaceOf(st.Overlay.PaneID)) {
			out.Panes = append(out.Panes, info)
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
	return api.Failure(id, api.ErrPaneNotFound, "caller is not inside an ikesu pane")
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
	if st.Overlay != nil {
		if p := s.app.Supervisor().Get(st.Overlay.PaneID); p != nil {
			shells[p.ShellPID()] = st.Overlay.PaneID
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
	if format != "text" && format != "ansi" {
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
	lines, err := pane.ReadLines(source, p.StripANSI && format != "ansi")
	if err != nil {
		return api.Failure(id, api.ErrInvalidRequest, err.Error())
	}
	var truncated bool
	// Trailing blank rows carry no information (herdr trims them the same
	// way), so a mostly-empty screen reads as its content, not blank space.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
		truncated = true
	}
	separator := "\n"
	if format == "ansi" {
		separator = "\r\n"
	}
	text := strings.Join(lines, separator)
	// The 0.2.24 physical-row reads end with a newline; logical reads do not.
	if text != "" && (source == "visible" || source == "recent") {
		text += separator
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
			Text:        text,
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
	// Implicit splits target the traversal-last leaf of the tab and append
	// to logical order (SPEC G1); explicit target_pane_id splits that pane.
	wsIdx := st.Active
	targetTab := st.Workspaces[st.Active].ActiveTab
	implicit := true
	target := st.Workspaces[st.Active].Tabs[targetTab].Layout.LastLeaf()
	if p.WorkspaceID != nil && *p.WorkspaceID != "" {
		wsIdx = s.findWorkspace(*p.WorkspaceID)
		if wsIdx < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
		ws := st.Workspaces[wsIdx]
		targetTab = ws.ActiveTab
		target = ws.Tabs[targetTab].Layout.LastLeaf()
	}
	if p.TargetPaneID != nil && *p.TargetPaneID != "" {
		pid, idx, ti, ok := s.findPane(*p.TargetPaneID)
		if !ok {
			return s.paneNotFound(id, *p.TargetPaneID)
		}
		target, wsIdx, targetTab = pid, idx, ti
		implicit = false
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
	var newID int
	if implicit {
		s.app.ExecCmd(model.CmdSplitImplicit{
			Workspace: wsIdx, Tab: targetTab, Dir: dir,
			Ratio: ratio, Focus: p.Focus, Cwd: cwd, Env: p.Env,
		})
		pids := st.Workspaces[wsIdx].Tabs[targetTab].Layout.PaneIDs()
		newID = pids[len(pids)-1]
	} else {
		s.app.ExecCmd(model.CmdSplitPane{
			Workspace: wsIdx, Pane: target, Dir: dir,
			Ratio: ratio, Focus: p.Focus, Cwd: cwd, Env: p.Env,
		})
		// New pane is inserted right after target in the tab's logical
		// order (PaneIDs keeps creation order).
		pids := st.Workspaces[wsIdx].Tabs[targetTab].Layout.PaneIDs()
		newID = pids[len(pids)-1]
		for i, v := range pids {
			if v == target && i+1 < len(pids) {
				newID = pids[i+1]
			}
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
	pane.RecordExternalInput()
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
	pane.RecordExternalInput()
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
	pane.RecordExternalInput()
	s.sendInputToPane(pane, p.Text, keys)
	return api.Success(id, api.OkResult{Type: "ok"})
}

// sendInputToPane writes text then, after the required 500 ms settle
// window, sends the keys — the agent CLIs rely on the gap so TUI apps
// have consumed the prompt before Enter arrives. Runs asynchronously so
// the app loop never blocks.
func (s *Server) sendInputToPane(pane *runtime.Pane, text string, keys []uv.KeyEvent) {
	pane.RecordExternalInput()
	go func() {
		if text != "" {
			pane.RecordExternalInput()
			pane.WithEmu(func(e *vt.Emulator) { e.Paste(text) })
		}
		if len(keys) == 0 {
			return
		}
		if text != "" {
			time.Sleep(500 * time.Millisecond)
		}
		pane.RecordExternalInput()
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
	s.app.SetReportedSource(pid, p.Agent, p.State, p.Source)
	if launch := s.agentLaunches[pid]; launch != nil {
		pane := s.sup.Get(pid)
		if pane != nil {
			s.agentReadiness(pid, p.Agent, api.AgentStatus(p.State), pane.Snapshot().Dead)
		}
	}
	if s.reportSources == nil {
		s.reportSources = map[int]string{}
	}
	if p.Agent == "" && p.State == "" {
		delete(s.reportSources, pid)
	} else {
		s.reportSources[pid] = p.Source
	}

	// Session refs from an explicit report are the most trusted source
	// (SPEC G3). agent_session_id/agent_session_path carry the native
	// structured ref; session_id is the shorthand for kind=id. An unsafe
	// reported value is never recorded — it stays out of the ledger and
	// does not mark the session dirty.
	var kind, value string
	switch {
	case p.AgentSessionID != nil && *p.AgentSessionID != "":
		kind, value = "id", *p.AgentSessionID
	case p.AgentSessionPath != nil && *p.AgentSessionPath != "":
		kind, value = "path", *p.AgentSessionPath
	case p.SessionID != nil && *p.SessionID != "":
		kind, value = "id", *p.SessionID
	}
	title := ""
	if p.Title != nil {
		// Blank/whitespace clears the presentation task title.
		title = strings.TrimSpace(*p.Title)
	}
	safe := kind != "" && session.SafeSessionValue(kind, value)
	switch {
	case safe:
		rec, _ := s.app.SessionRecord(pid)
		rec.Agent, rec.Kind, rec.Value = p.Agent, kind, value
		rec.SessionStartSource = ""
		rec.Source = "report:" + p.Source
		if p.Title != nil {
			rec.Title = title
		}
		s.app.RecordSession(pid, rec)
		s.ledgerPut(pid, rec)
	case p.Title != nil:
		// Report without a usable session ref still carries the task
		// title; keep any existing session value untouched.
		rec, _ := s.app.SessionRecord(pid)
		rec.Agent = p.Agent
		rec.Title = title
		s.app.RecordSession(pid, rec)
		if rec.Value != "" {
			s.ledgerPut(pid, rec)
		}
	}
	return api.Success(id, api.OkResult{Type: "ok"})
}

// --- pane info methods ---

// resolvePane defaults an empty pane ref to the focused pane.
func (s *Server) resolvePane(ref string) (pid, wsIdx, tabIdx int, ok bool) {
	if ref == "" {
		st := s.app.State()
		ws := st.ActiveWorkspace()
		if ws == nil {
			return 0, -1, -1, false
		}
		t := ws.TabRef()
		if t == nil {
			return 0, -1, -1, false
		}
		return t.Layout.Focused(), st.Active, ws.ActiveTab, true
	}
	return s.findPane(ref)
}
