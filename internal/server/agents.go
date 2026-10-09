package server

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
)

// agentInfoAt builds the AgentInfo view for a pane. Name is the manual
// label (pane rename / agent rename); Agent is the detected or reported
// agent kind (claude, codex, ...). gtx has no global/short pane id split:
// all id fields carry the public pN id.
func (s *Server) agentInfoAt(pid, wsIdx, tabIdx int) api.AgentInfo {
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	tab := ws.Tabs[tabIdx]

	var sn runtime.Snapshot
	if p := s.sup.Get(pid); p != nil {
		sn = p.Snapshot()
	}
	agent := sn.AgentID
	if rep, _, ok := s.app.Reported(pid); ok && rep != "" {
		agent = rep
	}
	name := st.PaneLabels[pid]
	display := name
	if display == "" {
		display = agent
	}
	cwd := sn.Cwd
	if cwd == "" {
		cwd = ws.Cwd
	}
	status := s.app.PaneStatusKey(pid)
	paneID := fmtPaneID(pid)
	return api.AgentInfo{
		TerminalID:     paneID,
		AgentStatus:    api.AgentStatus(status),
		WorkspaceID:    fmtWorkspaceID(ws.ID),
		TabID:          fmtTabID(ws.ID, tab.ID),
		ShortPaneID:    paneID,
		GlobalPaneID:   paneID,
		GlobalPaneNum:  pid,
		PaneID:         paneID,
		Focused:        wsIdx == st.Active && ws.ActiveTab == tabIdx && tab.Layout.Focused() == pid,
		Revision:       sn.Revision,
		Name:           optString(name),
		Agent:          optString(agent),
		DisplayAgent:   optString(display),
		Cwd:            optString(cwd),
		ForegroundCwd:  optString(sn.Cwd),
		TerminalTitle:  optString(sn.OSCTitle),
		InteractiveRdy: status != "blocked",
	}
}

// agentList returns infos for panes that host an agent.
func (s *Server) agentList(id string) api.Response {
	out := api.AgentListResult{Type: "agent_list", Agents: []api.AgentInfo{}}
	s.eachPane(func(pid, wsIdx, tabIdx int) {
		if s.isAgentPane(pid) {
			out.Agents = append(out.Agents, s.agentInfoAt(pid, wsIdx, tabIdx))
		}
	})
	return api.Success(id, out)
}

// agentGet resolves a target (pane id or agent name) to its info.
func (s *Server) agentGet(id, target string) api.Response {
	t, err := s.resolveTarget(target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	return api.Success(id, api.AgentInfoResult{
		Type:  "agent_info",
		Agent: s.agentInfoAt(t.pid, t.wsIdx, t.tabIdx),
	})
}

// agentFocus moves focus to the resolved agent pane.
func (s *Server) agentFocus(id, target string) api.Response {
	t, err := s.resolveTarget(target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	st := s.app.State()
	if t.wsIdx != st.Active {
		s.app.ExecCmd(model.CmdSelectWorkspace{Index: t.wsIdx})
	}
	ws := st.Workspaces[t.wsIdx]
	if ws.ActiveTab != t.tabIdx {
		s.app.ExecCmd(model.CmdSelectTab{Workspace: t.wsIdx, Tab: t.tabIdx})
	}
	s.app.ExecCmd(model.CmdFocusPane{Pane: t.pid})
	return api.Success(id, api.AgentInfoResult{
		Type:  "agent_info",
		Agent: s.agentInfoAt(t.pid, t.wsIdx, t.tabIdx),
	})
}

// agentRename sets (or clears, when name is null) the pane label — the
// pane's mailbox name, mirroring herdr's agent_name/manual_label pair.
func (s *Server) agentRename(id string, p api.AgentRenameParams) api.Response {
	t, err := s.resolveTarget(p.Target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	name := ""
	if p.Name != nil {
		name = *p.Name
	}
	if name != "" {
		var dup []string
		s.eachPane(func(pid, _, _ int) {
			if pid != t.pid && s.agentNameOf(pid) == name {
				dup = append(dup, fmtPaneID(pid))
			}
		})
		if len(dup) > 0 {
			return api.Failure(id, api.ErrAmbiguous,
				fmt.Sprintf("agent name %q is already used by %s", name, dup[0]))
		}
	}
	s.app.ExecCmd(model.CmdRenamePane{Pane: t.pid, Label: name})
	return api.Success(id, api.AgentInfoResult{
		Type:  "agent_info",
		Agent: s.agentInfoAt(t.pid, t.wsIdx, t.tabIdx),
	})
}

// agentRead delegates to pane.read on the resolved agent pane.
func (s *Server) agentRead(id string, p api.AgentReadParams) api.Response {
	t, err := s.resolveTarget(p.Target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	return s.paneRead(id, api.PaneReadParams{
		PaneID: fmtPaneID(t.pid), Source: p.Source, Lines: p.Lines,
		Format: p.Format, StripANSI: p.StripANSI,
	})
}

// agentSend submits text to an agent pane: text, 500 ms settle, Enter.
func (s *Server) agentSend(id string, p api.AgentSendParams) api.Response {
	t, err := s.resolveTarget(p.Target, true)
	if err != nil {
		return api.Failure(id, err.Code, err.Message)
	}
	pane := s.sup.Get(t.pid)
	if pane == nil {
		return s.paneNotFound(id, p.Target)
	}
	s.sendInputToPane(pane, p.Text, []uv.KeyEvent{uv.KeyPressEvent{Code: uv.KeyEnter}})
	return api.Success(id, api.OkResult{Type: "ok"})
}

// agentStart opens a pane running argv directly (a real agent process,
// not a shell wrapper), in the requested workspace/tab or a split.
func (s *Server) agentStart(id string, p api.AgentStartParams) api.Response {
	if len(p.Argv) == 0 {
		return api.Failure(id, api.ErrInvalidRequest, "argv must not be empty")
	}
	st := s.app.State()
	wsIdx := st.Active
	if p.WorkspaceID != nil && *p.WorkspaceID != "" {
		wsIdx = s.findWorkspace(*p.WorkspaceID)
		if wsIdx < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
	}
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if cwd == "" {
		cwd = st.Workspaces[wsIdx].Cwd
	}
	if _, _, err := s.sup.ResolveCommand(p.Argv, cwd); err != nil {
		return api.Failure(id, api.ErrInvalidRequest, err.Error())
	}
	env := p.Env
	var pid, tabIdx int
	if p.Split != nil && *p.Split != "" {
		var dir layout.Direction
		switch *p.Split {
		case "right", "vertical":
			dir = layout.Vertical
		case "down", "horizontal":
			dir = layout.Horizontal
		default:
			return api.Failure(id, api.ErrInvalidRequest,
				fmt.Sprintf("invalid split direction %q", *p.Split))
		}
		ws := st.Workspaces[wsIdx]
		tabIdx = ws.ActiveTab
		if p.TabID != nil && *p.TabID != "" {
			wi, ti := s.findTab(*p.TabID)
			if wi < 0 {
				return s.tabNotFound(id, *p.TabID)
			}
			wsIdx, tabIdx = wi, ti
			ws = st.Workspaces[wsIdx]
		}
		// Implicit pane add: split the traversal-last leaf, appended to
		// logical order (SPEC G1).
		s.app.ExecCmd(model.CmdSplitImplicit{
			Workspace: wsIdx, Tab: tabIdx, Dir: dir,
			Ratio: 0.5, Focus: p.Focus, Cwd: cwd, Env: env, Argv: p.Argv,
		})
		pids := ws.Tabs[tabIdx].Layout.PaneIDs()
		if len(pids) == 0 {
			return api.Failure(id, api.ErrInvalidRequest, "split failed")
		}
		pid = pids[len(pids)-1]
	} else {
		if p.TabID != nil && *p.TabID != "" {
			return api.Failure(id, api.ErrInvalidRequest,
				"--tab requires --split (a tab holds panes; a new pane without --split opens a new tab)")
		}
		s.app.ExecCmd(model.CmdNewTab{
			Workspace: wsIdx, Cwd: cwd, Env: env, NoFocus: !p.Focus, Argv: p.Argv,
		})
		ws := st.Workspaces[wsIdx]
		tabIdx = len(ws.Tabs) - 1
		pid = ws.Tabs[tabIdx].Layout.Focused()
	}
	if s.sup.Get(pid) == nil {
		s.app.ExecCmd(model.CmdClosePane{Pane: model.PaneID(pid)})
		return api.Failure(id, api.ErrInvalidRequest, "agent process could not start")
	}
	if p.Name != "" {
		s.app.ExecCmd(model.CmdRenamePane{Pane: model.PaneID(pid), Label: p.Name})
	}
	return api.Success(id, api.AgentInfoResult{
		Type:  "agent_info",
		Agent: s.agentInfoAt(pid, wsIdx, tabIdx),
	})
}

// shellEnvQuote is the server-local shell quoting for typed lines.
func shellEnvQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '=' || r == '+' || r == ',' || r == '@' || r == '%') {
			safe = false
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
