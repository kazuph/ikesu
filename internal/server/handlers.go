package server

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/runtime"
)

// dispatchOnLoop runs on the app goroutine; it may touch st/sup safely.
func (s *Server) dispatchOnLoop(req *api.Request) api.Response {
	switch req.Method {
	case api.MethodWorkspaceList:
		var p api.EmptyParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsList(req.ID)
	case api.MethodWorkspaceGet:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsGet(req.ID, p.WorkspaceID)
	case api.MethodWorkspaceCreate:
		var p api.WorkspaceCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsCreate(req.ID, p)
	case api.MethodWorkspaceFocus:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsFocus(req.ID, p.WorkspaceID)
	case api.MethodWorkspaceRename:
		var p api.WorkspaceRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsRename(req.ID, p)
	case api.MethodWorkspaceClose:
		var p api.WorkspaceTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.wsClose(req.ID, p.WorkspaceID)

	case api.MethodPaneList:
		var p api.PaneListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneList(req.ID, p)
	case api.MethodPaneGet:
		var p api.PaneTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneGet(req.ID, p.PaneID)
	case api.MethodPaneCurrent:
		var p api.PaneCurrentParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneCurrent(req.ID, p)
	case api.MethodPaneFocus:
		var p api.PaneFocusParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneFocus(req.ID, p.PaneID)
	case api.MethodPaneRename:
		var p api.PaneRenameParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneRename(req.ID, p)
	case api.MethodPaneRead:
		var p api.PaneReadParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneRead(req.ID, p)
	case api.MethodPaneSplit:
		var p api.PaneSplitParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSplit(req.ID, p)
	case api.MethodPaneClose:
		var p api.PaneTarget
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneClose(req.ID, p.PaneID)
	case api.MethodPaneSendText:
		var p api.PaneSendTextParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendText(req.ID, p)
	case api.MethodPaneSendKeys:
		var p api.PaneSendKeysParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendKeys(req.ID, p)
	case api.MethodPaneSendInput:
		var p api.PaneSendInputParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneSendInput(req.ID, p)
	case api.MethodPaneReportAgent:
		var p api.PaneReportAgentParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneReportAgent(req.ID, p)

	case api.MethodPaneLayout:
		var p api.PaneQueryParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneLayout(req.ID, p.PaneID)
	case api.MethodPaneProcessInfo:
		var p api.PaneQueryParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneProcessInfo(req.ID, p.PaneID)
	case api.MethodPaneEdges:
		var p api.PaneQueryParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneEdges(req.ID, p.PaneID)
	case api.MethodPaneNeighbor:
		var p api.PaneNeighborParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneNeighbor(req.ID, p)
	case api.MethodPaneResize:
		var p api.PaneResizeParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		return s.paneResize(req.ID, p)
	}
	return api.Failure(req.ID, api.ErrUnknownMethod,
		fmt.Sprintf("unknown method %q", req.Method))
}

// --- lookups ---

// findPane locates a pane by public id and returns its workspace index.
func (s *Server) findPane(paneRef string) (pid int, wsIdx int, ok bool) {
	n, valid := parsePaneID(paneRef)
	if !valid {
		return 0, -1, false
	}
	wsIdx = s.app.State().WorkspaceOf(n)
	return n, wsIdx, wsIdx >= 0
}

// findWorkspace resolves a public workspace id to its index.
func (s *Server) findWorkspace(ref string) int {
	id, ok := parseWorkspaceID(ref)
	if !ok {
		return -1
	}
	for i, ws := range s.app.State().Workspaces {
		if ws.ID == id {
			return i
		}
	}
	return -1
}

func (s *Server) paneNotFound(id, ref string) api.Response {
	return api.Failure(id, api.ErrPaneNotFound, fmt.Sprintf("pane not found: %s", ref))
}

func (s *Server) wsNotFound(id, ref string) api.Response {
	return api.Failure(id, api.ErrWorkspaceNotFound, fmt.Sprintf("workspace not found: %s", ref))
}

// --- info builders ---

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func tabIDFor(wsID int) string { return fmt.Sprintf("w%d:t1", wsID) }

// wsAgentStatus aggregates pane statuses: blocked > working > done > idle.
func (s *Server) wsAgentStatus(ws *model.Workspace) api.AgentStatus {
	best := api.AgentUnknown
	prio := map[string]int{"idle": 1, "done": 2, "working": 3, "blocked": 4}
	for _, pid := range ws.Layout.PaneIDs() {
		k := s.app.PaneStatusKey(pid)
		if prio[k] > prio[best] {
			best = k
		}
	}
	return best
}

func (s *Server) wsInfo(i int) api.WorkspaceInfo {
	st := s.app.State()
	ws := st.Workspaces[i]
	wsID := fmtWorkspaceID(ws.ID)
	return api.WorkspaceInfo{
		WorkspaceID: wsID,
		Number:      i + 1,
		Label:       ws.Name,
		Focused:     i == st.Active,
		PaneCount:   ws.Layout.PaneCount(),
		TabCount:    1,
		ActiveTabID: fmt.Sprintf("%s:t1", wsID),
		AgentStatus: s.wsAgentStatus(ws),
	}
}

func (s *Server) paneInfo(pid int) (api.PaneInfo, bool) {
	st := s.app.State()
	wsIdx := st.WorkspaceOf(pid)
	if wsIdx < 0 {
		return api.PaneInfo{}, false
	}
	ws := st.Workspaces[wsIdx]
	wsID := fmtWorkspaceID(ws.ID)

	var sn runtime.Snapshot
	if p := s.app.Supervisor().Get(pid); p != nil {
		sn = p.Snapshot()
	}
	agent := sn.AgentID
	if rep, _, ok := s.app.Reported(pid); ok && rep != "" {
		agent = rep
	}
	cwd := sn.Cwd
	if cwd == "" {
		cwd = ws.Cwd
	}
	rows := 0
	for _, pr := range ws.Layout.Panes(st.PanesArea()) {
		if pr.ID == pid {
			rows = max(pr.Rect.H-2, 1)
		}
	}
	return api.PaneInfo{
		PaneID:        fmtPaneID(pid),
		WorkspaceID:   wsID,
		TabID:         fmt.Sprintf("%s:t1", wsID),
		TerminalID:    fmtPaneID(pid),
		Focused:       wsIdx == st.Active && ws.Layout.Focused() == pid,
		AgentStatus:   s.app.PaneStatusKey(pid),
		Agent:         optString(agent),
		Cwd:           optString(cwd),
		ForegroundCwd: optString(sn.Cwd),
		Label:         optString(st.PaneLabels[pid]),
		TerminalTitle: optString(sn.OSCTitle),
		Revision:      sn.Revision,
		Scroll: &api.PaneScrollInfo{
			OffsetFromBottom:    s.app.ScrollOffset(pid),
			MaxOffsetFromBottom: sn.ScrollbackLen,
			ViewportRows:        rows,
		},
	}, true
}

// layoutSnapshot renders the workspace's pane rects and split borders.
func (s *Server) layoutSnapshot(wsIdx int) api.PaneLayoutSnapshot {
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	area := st.PanesArea()
	wsID := fmtWorkspaceID(ws.ID)
	snap := api.PaneLayoutSnapshot{
		WorkspaceID:   wsID,
		TabID:         fmt.Sprintf("%s:t1", wsID),
		Area:          api.PaneLayoutRect{X: area.X, Y: area.Y, Width: area.W, Height: area.H},
		FocusedPaneID: fmtPaneID(ws.Layout.Focused()),
		Panes:         []api.PaneLayoutPane{},
		Splits:        []api.PaneLayoutSplit{},
	}
	for _, pr := range ws.Layout.Panes(area) {
		snap.Panes = append(snap.Panes, api.PaneLayoutPane{
			PaneID:  fmtPaneID(pr.ID),
			Focused: pr.ID == ws.Layout.Focused(),
			Rect:    api.PaneLayoutRect{X: pr.Rect.X, Y: pr.Rect.Y, Width: pr.Rect.W, Height: pr.Rect.H},
		})
	}
	for i, b := range ws.Layout.Splits(area) {
		dir := "right"
		if b.Direction == layout.Horizontal {
			dir = "down"
		}
		snap.Splits = append(snap.Splits, api.PaneLayoutSplit{
			ID:        fmt.Sprintf("%s:s%d", wsID, i+1),
			Direction: dir,
			Ratio:     b.Ratio,
			Rect: api.PaneLayoutRect{
				X: b.Area.X, Y: b.Area.Y, Width: b.Area.W, Height: b.Area.H,
			},
		})
	}
	return snap
}

// --- workspace handlers ---

func (s *Server) wsList(id string) api.Response {
	st := s.app.State()
	out := api.WorkspaceListResult{Type: "workspace_list", Workspaces: []api.WorkspaceInfo{}}
	for i := range st.Workspaces {
		out.Workspaces = append(out.Workspaces, s.wsInfo(i))
	}
	return api.Success(id, out)
}

func (s *Server) wsGet(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsCreate(id string, p api.WorkspaceCreateParams) api.Response {
	st := s.app.State()
	cwd := ""
	if p.Cwd != nil {
		cwd = *p.Cwd
	}
	if cwd == "" {
		cwd = s.cwd
	}
	s.app.ExecCmd(model.CmdNewWorkspace{Cwd: cwd, Env: p.Env, NoFocus: !p.Focus})
	idx := len(st.Workspaces) - 1
	ws := st.Workspaces[idx]
	if p.Label != nil && *p.Label != "" {
		s.app.ExecCmd(model.CmdRenameWorkspace{Index: idx, Name: *p.Label})
	}
	root := ws.Layout.Focused()
	info, _ := s.paneInfo(root)
	tab := api.TabInfo{
		TabID:       fmt.Sprintf("%s:t1", fmtWorkspaceID(ws.ID)),
		WorkspaceID: fmtWorkspaceID(ws.ID),
		Label:       "",
		Number:      1,
		Focused:     p.Focus,
		PaneCount:   1,
		AgentStatus: api.AgentUnknown,
	}
	return api.Success(id, api.WorkspaceCreatedResult{
		Type:      "workspace_created",
		Workspace: s.wsInfo(idx),
		Tab:       tab,
		RootPane:  info,
	})
}

func (s *Server) wsFocus(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdSelectWorkspace{Index: i})
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsRename(id string, p api.WorkspaceRenameParams) api.Response {
	i := s.findWorkspace(p.WorkspaceID)
	if i < 0 {
		return s.wsNotFound(id, p.WorkspaceID)
	}
	s.app.ExecCmd(model.CmdRenameWorkspace{Index: i, Name: p.Label})
	return api.Success(id, api.WorkspaceInfoResult{Type: "workspace_info", Workspace: s.wsInfo(i)})
}

func (s *Server) wsClose(id, ref string) api.Response {
	i := s.findWorkspace(ref)
	if i < 0 {
		return s.wsNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdCloseWorkspace{Index: i})
	return api.Success(id, api.OkResult{Type: "ok"})
}

// --- pane handlers ---

func (s *Server) paneList(id string, p api.PaneListParams) api.Response {
	st := s.app.State()
	out := api.PaneListResult{Type: "pane_list", Panes: []api.PaneInfo{}}
	for i, ws := range st.Workspaces {
		if p.WorkspaceID != nil && *p.WorkspaceID != "" {
			if s.findWorkspace(*p.WorkspaceID) != i {
				continue
			}
		}
		for _, pid := range ws.Layout.PaneIDs() {
			if info, ok := s.paneInfo(pid); ok {
				out.Panes = append(out.Panes, info)
			}
		}
	}
	return api.Success(id, out)
}

func (s *Server) paneGet(id, ref string) api.Response {
	pid, _, ok := s.findPane(ref)
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
		pid, _, ok := s.findPane(*p.CallerPaneID)
		if !ok {
			return s.paneNotFound(id, *p.CallerPaneID)
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
		for _, id := range ws.Layout.PaneIDs() {
			if p := s.app.Supervisor().Get(id); p != nil {
				if sp := p.ShellPID(); sp > 0 {
					shells[sp] = id
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
	pid, wsIdx, ok := s.findPane(ref)
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
	pid, _, ok := s.findPane(p.PaneID)
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
	pid, wsIdx, ok := s.findPane(p.PaneID)
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
			fmt.Sprintf("unknown read format %q", format))
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
	if format == "ansi" {
		screen, scroll = pane.ScreenANSI, pane.ScrollbackANSI
	}
	var lines []string
	var truncated bool
	switch source {
	case "visible", "detection":
		// "detection" approximates the detector's view with the visible
		// screen; gtx detection is process/OSC based, not buffer based.
		lines = screen()
	case "recent", "recent_unwrapped", "recent-unwrapped":
		// recent_unwrapped falls back to physical lines: the vt emulator
		// does not record wrap flags on scrollback rows.
		lines = append(scroll(), screen()...)
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
	wsID := fmtWorkspaceID(s.app.State().Workspaces[wsIdx].ID)
	return api.Success(id, api.PaneReadResultEnvelope{
		Type: "pane_read",
		Read: api.PaneReadResult{
			PaneID:      fmtPaneID(pid),
			WorkspaceID: wsID,
			TabID:       fmt.Sprintf("%s:t1", wsID),
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
	wsIdx := st.Active
	if p.WorkspaceID != nil && *p.WorkspaceID != "" {
		wsIdx = s.findWorkspace(*p.WorkspaceID)
		if wsIdx < 0 {
			return s.wsNotFound(id, *p.WorkspaceID)
		}
		target = st.Workspaces[wsIdx].Layout.Focused()
	}
	if p.TargetPaneID != nil && *p.TargetPaneID != "" {
		pid, idx, ok := s.findPane(*p.TargetPaneID)
		if !ok {
			return s.paneNotFound(id, *p.TargetPaneID)
		}
		target, wsIdx = pid, idx
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
	// New pane is the one just allocated: the last id in the workspace.
	pids := st.Workspaces[wsIdx].Layout.PaneIDs()
	if len(pids) == 0 {
		return api.Failure(id, api.ErrInvalidRequest, "split failed")
	}
	newID := pids[len(pids)-1]
	// PaneIDs keeps creation order; the split inserts right after target.
	for i, v := range pids {
		if v == target && i+1 < len(pids) {
			newID = pids[i+1]
		}
	}
	info, _ := s.paneInfo(newID)
	return api.Success(id, api.PaneInfoResult{Type: "pane_info", Pane: info})
}

func (s *Server) paneClose(id, ref string) api.Response {
	pid, _, ok := s.findPane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	s.app.ExecCmd(model.CmdClosePane{Pane: pid})
	return api.Success(id, api.OkResult{Type: "ok"})
}

func (s *Server) paneSendText(id string, p api.PaneSendTextParams) api.Response {
	pid, _, ok := s.findPane(p.PaneID)
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
	pid, _, ok := s.findPane(p.PaneID)
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
	pid, _, ok := s.findPane(p.PaneID)
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
	pane.WithEmu(func(e *vt.Emulator) {
		if p.Text != "" {
			e.Paste(p.Text)
		}
		e.SendKeys(keys...)
	})
	return api.Success(id, api.OkResult{Type: "ok"})
}

func (s *Server) paneReportAgent(id string, p api.PaneReportAgentParams) api.Response {
	pid, _, ok := s.findPane(p.PaneID)
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
func (s *Server) resolvePane(ref string) (pid int, wsIdx int, ok bool) {
	if ref == "" {
		st := s.app.State()
		if ws := st.ActiveWorkspace(); ws != nil {
			return ws.Layout.Focused(), st.Active, true
		}
		return 0, -1, false
	}
	return s.findPane(ref)
}

func (s *Server) paneLayout(id, ref string) api.Response {
	_, wsIdx, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	return api.Success(id, api.PaneLayoutResult{Type: "pane_layout", Layout: s.layoutSnapshot(wsIdx)})
}

func (s *Server) paneProcessInfo(id, ref string) api.Response {
	pid, _, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	pane := s.app.Supervisor().Get(pid)
	if pane == nil {
		return s.paneNotFound(id, ref)
	}
	job := pane.ForegroundJob()
	res := api.PaneProcessInfoResult{
		Type:                "pane_process_info",
		PaneID:              fmtPaneID(pid),
		ForegroundProcesses: []api.PaneProcessInfoProcess{},
	}
	if sp := pane.ShellPID(); sp > 0 {
		res.ShellPID = &sp
	}
	if job.ProcessGroupID > 0 {
		pgid := job.ProcessGroupID
		res.ForegroundProcessGroupID = &pgid
	}
	for _, pr := range job.Processes {
		proc := api.PaneProcessInfoProcess{PID: pr.PID, Name: pr.Name, Argv: pr.Argv}
		if len(pr.Argv) > 0 {
			a0 := pr.Argv[0]
			proc.Argv0 = &a0
		}
		if pr.Cmdline != "" {
			cl := pr.Cmdline
			proc.Cmdline = &cl
		}
		if c := platform.ProcCwd(pr.PID); c != "" {
			proc.Cwd = &c
		}
		res.ForegroundProcesses = append(res.ForegroundProcesses, proc)
	}
	return api.Success(id, res)
}

func (s *Server) paneEdges(id, ref string) api.Response {
	pid, wsIdx, ok := s.resolvePane(ref)
	if !ok {
		return s.paneNotFound(id, ref)
	}
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	area := st.PanesArea()
	var me layout.Rect
	found := false
	for _, pr := range ws.Layout.Panes(area) {
		if pr.ID == pid {
			me, found = pr.Rect, true
		}
	}
	res := api.PaneEdgesResult{Type: "pane_edges", PaneID: fmtPaneID(pid), Layout: s.layoutSnapshot(wsIdx)}
	if !found {
		return api.Success(id, res)
	}
	for _, pr := range ws.Layout.Panes(area) {
		if pr.ID == pid {
			continue
		}
		r := pr.Rect
		switch {
		case r.X+r.W == me.X && rangesOverlap(r.Y, r.H, me.Y, me.H):
			res.Left = true
		case me.X+me.W == r.X && rangesOverlap(r.Y, r.H, me.Y, me.H):
			res.Right = true
		case r.Y+r.H == me.Y && rangesOverlap(r.X, r.W, me.X, me.W):
			res.Up = true
		case me.Y+me.H == r.Y && rangesOverlap(r.X, r.W, me.X, me.W):
			res.Down = true
		}
	}
	return api.Success(id, res)
}

func (s *Server) paneNeighbor(id string, p api.PaneNeighborParams) api.Response {
	pid, wsIdx, ok := s.resolvePane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	area := st.PanesArea()
	res := api.PaneNeighborResult{
		Type: "pane_neighbor", PaneID: fmtPaneID(pid),
		Direction: p.Direction, Layout: s.layoutSnapshot(wsIdx),
	}
	if n := neighborPane(ws, pid, p.Direction, area); n >= 0 {
		id := fmtPaneID(n)
		res.NeighborPaneID = &id
	}
	return api.Success(id, res)
}

// neighborPane finds the pane adjacent to pid in direction (same picking
// rule as Layout.FocusDir: shared edge + closest centre).
func neighborPane(ws *model.Workspace, pid int, dir string, area layout.Rect) int {
	panes := ws.Layout.Panes(area)
	var me layout.Rect
	found := false
	for _, pr := range panes {
		if pr.ID == pid {
			me, found = pr.Rect, true
		}
	}
	if !found {
		return -1
	}
	best, bestDist := -1, 0.0
	mcx, mcy := center(me)
	for _, pr := range panes {
		if pr.ID == pid {
			continue
		}
		r := pr.Rect
		var ok bool
		switch dir {
		case "left":
			ok = r.X+r.W == me.X && rangesOverlap(r.Y, r.H, me.Y, me.H)
		case "right":
			ok = me.X+me.W == r.X && rangesOverlap(r.Y, r.H, me.Y, me.H)
		case "up":
			ok = r.Y+r.H == me.Y && rangesOverlap(r.X, r.W, me.X, me.W)
		case "down":
			ok = me.Y+me.H == r.Y && rangesOverlap(r.X, r.W, me.X, me.W)
		}
		if !ok {
			continue
		}
		cx, cy := center(r)
		d := (cx-mcx)*(cx-mcx) + (cy-mcy)*(cy-mcy)
		if best < 0 || d < bestDist {
			best, bestDist = pr.ID, d
		}
	}
	return best
}

func (s *Server) paneResize(id string, p api.PaneResizeParams) api.Response {
	pid, wsIdx, ok := s.resolvePane(p.PaneID)
	if !ok {
		return s.paneNotFound(id, p.PaneID)
	}
	switch p.Direction {
	case "left", "right", "up", "down":
	default:
		return api.Failure(id, api.ErrInvalidRequest,
			fmt.Sprintf("invalid direction %q", p.Direction))
	}
	st := s.app.State()
	ws := st.Workspaces[wsIdx]
	area := st.PanesArea()
	res := api.PaneResizeResult{
		Type: "pane_resize", PaneID: fmtPaneID(pid),
		FocusedPaneID: fmtPaneID(ws.Layout.Focused()),
	}
	// The pane's border in `direction` is a split divider we can nudge.
	var me layout.Rect
	for _, pr := range ws.Layout.Panes(area) {
		if pr.ID == pid {
			me = pr.Rect
		}
	}
	amount := 0.05
	if p.Amount != nil {
		amount = *p.Amount
	}
	for _, b := range ws.Layout.Splits(area) {
		var match bool
		var delta float64
		switch p.Direction {
		case "right":
			// pane's right edge abuts a vertical divider
			match = b.Direction == layout.Vertical && b.Pos == me.X+me.W
			delta = amount / float64(max(b.Area.W, 1))
		case "left":
			match = b.Direction == layout.Vertical && b.Pos == me.X
			delta = -amount / float64(max(b.Area.W, 1))
		case "down":
			match = b.Direction == layout.Horizontal && b.Pos == me.Y+me.H
			delta = amount / float64(max(b.Area.H, 1))
		case "up":
			match = b.Direction == layout.Horizontal && b.Pos == me.Y
			delta = -amount / float64(max(b.Area.H, 1))
		}
		if match {
			if ws.Layout.SetRatioAt(b.Path, b.Ratio+delta) {
				res.Changed = true
			}
			break
		}
	}
	if res.Changed {
		// Trigger a layout sync so PTYs resize.
		s.app.ExecCmd(model.CmdResize{Cols: st.Cols, Rows: st.Rows})
	} else {
		reason := "unchanged"
		res.Reason = &reason
	}
	res.Layout = s.layoutSnapshot(wsIdx)
	return api.Success(id, res)
}

// --- helpers ---

func rangesOverlap(s1, l1, s2, l2 int) bool { return s1 < s2+l2 && s2 < s1+l1 }

func center(r layout.Rect) (float64, float64) {
	return float64(r.X) + float64(r.W)/2, float64(r.Y) + float64(r.H)/2
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// parseKeys converts key specs to events, returning the first bad name.
func parseKeys(keys []string) ([]uv.KeyEvent, string) {
	var out []uv.KeyEvent
	for _, k := range keys {
		ev, ok := parseKeyName(k)
		if !ok {
			return nil, k
		}
		out = append(out, ev)
	}
	return out, ""
}
