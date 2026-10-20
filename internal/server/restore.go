package server

// Exact per-pane agent session restore (SPEC G3): observation of the
// session id a pane's agent is actually running (explicit report wins,
// then session-file match, then cmdline resume-shape), the pane-session
// ledger, the agent.restore plan, and the missing-session warnings used
// by the restart confirmation dialog. No path ever falls back to
// "--last", "resume --last", or a cwd/mtime-latest guess.

import (
	"fmt"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/tui"
)

// resumeSubmitDelay separates typing the resume command from the Enter
// keypress so the receiving shell has finished its prompt draw.
const resumeSubmitDelay = 150 * time.Millisecond

// --- ledger --------------------------------------------------------------

// ledgerEntry returns the ledger row for a pane in this session. When
// the DB file does not exist no ledger rows can exist either; avoid
// opening the store so reads stay free of the engine's memory cost.
func (s *Server) ledgerEntry(pid int) (dispatch.PaneSession, bool) {
	if !dispatch.DBExists(s.session) {
		return dispatch.PaneSession{}, false
	}
	st, err := s.db()
	if err != nil {
		return dispatch.PaneSession{}, false
	}
	rows, err := st.PaneSessions(s.session)
	if err != nil {
		return dispatch.PaneSession{}, false
	}
	for _, r := range rows {
		if r.PaneID == pid {
			return r, true
		}
	}
	return dispatch.PaneSession{}, false
}

// ledgerPut writes the pane's session record into the ledger. Call on the
// app loop (reads model state).
func (s *Server) ledgerPut(pid int, rec tui.AgentRecord) {
	st, err := s.db()
	if err != nil {
		return
	}
	stt := s.app.State()
	wsIdx, tabIdx := stt.FindPane(pid)
	wsID, tabID := 0, 0
	cwd := ""
	if wsIdx >= 0 {
		ws := stt.Workspaces[wsIdx]
		wsID = ws.ID
		if tabIdx >= 0 && tabIdx < len(ws.Tabs) {
			tabID = ws.Tabs[tabIdx].ID
		}
		cwd = ws.Cwd
	}
	if rp := s.sup.Get(pid); rp != nil {
		if c := rp.Snapshot().Cwd; c != "" {
			cwd = c
		}
	}
	_ = st.PutPaneSession(dispatch.PaneSession{
		Session:            s.session,
		PaneID:             pid,
		TerminalID:         fmtPaneID(pid),
		WorkspaceID:        wsID,
		TabID:              tabID,
		Cwd:                cwd,
		Agent:              rec.Agent,
		SessionKind:        rec.Kind,
		SessionID:          rec.Value,
		Title:              rec.Title,
		Source:             rec.Source,
		SessionStartSource: rec.SessionStartSource,
		ObservedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// ledgerDel drops the ledger row for a permanently closed pane.
func (s *Server) ledgerDel(pid int) {
	delete(s.observedStart, pid)
	if !dispatch.DBExists(s.session) {
		return
	}
	if st, err := s.db(); err == nil {
		_ = st.DeletePaneSession(s.session, pid)
	}
}

// --- session id resolution -------------------------------------------------

// paneSessionView merges the terminal-side record and the ledger row for
// one pane. Values are kept raw; `safe` marks which source produced a
// usable id and `reason` carries the warning wording when none did.
type paneSessionView struct {
	agent    string
	kind     string
	value    string
	title    string
	safe     bool
	reason   string // "invalid terminal session id" | "invalid ledger session id" | "missing session id"
	terminal bool   // terminal-side record exists
	ledger   bool   // same-agent ledger row exists
}

// resolvePaneSession computes the pane's effective session ref. Call on
// the app loop.
func (s *Server) resolvePaneSession(pid int) paneSessionView {
	var v paneSessionView
	rec, hasRec := s.app.SessionRecord(pid)
	led, hasLed := s.ledgerEntry(pid)
	v.terminal = hasRec
	v.ledger = hasLed && (!hasRec || led.Agent == "" || led.Agent == rec.Agent)

	agent := ""
	if hasRec {
		agent = rec.Agent
	}
	if agent == "" && hasLed {
		agent = led.Agent
	}
	if agent == "" {
		if rp := s.sup.Get(pid); rp != nil {
			agent = rp.Snapshot().AgentID
		}
	}
	v.agent = agent

	// Terminal record wins; a same-agent ledger row is the fallback that
	// keeps the exact id alive across a restarted terminal.
	if hasRec {
		v.kind, v.value, v.title = rec.Kind, rec.Value, rec.Title
		if session.SafeSessionValue(rec.Kind, rec.Value) {
			v.safe = true
			return v
		}
	}
	if hasLed && led.Agent == agent {
		if v.value == "" && led.SessionID != "" {
			v.kind, v.value = led.SessionKind, led.SessionID
		}
		if v.title == "" {
			v.title = led.Title
		}
		if session.SafeSessionValue(led.SessionKind, led.SessionID) {
			v.kind, v.value = led.SessionKind, led.SessionID
			v.safe = true
			return v
		}
	}
	switch {
	case hasRec && rec.Value != "":
		v.reason = "invalid terminal session id"
	case hasLed && led.Agent == agent && led.SessionID != "":
		v.reason = "invalid ledger session id"
	default:
		v.reason = "missing session id"
	}
	return v
}

// --- observation -----------------------------------------------------------

// observeAgentSessions fills missing session records for panes running a
// known agent. Call on the app loop.
func (s *Server) observeAgentSessions() {
	stt := s.app.State()
	for _, ws := range stt.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.Order() {
				s.observePaneSession(pid)
			}
		}
	}
}

// observePaneSession records the pane's running agent session id when it
// can be determined exactly: explicit reports win; otherwise a unique
// session-file match (cwd + start-time window); otherwise the process's
// own resume-shape cmdline. Anything ambiguous stays unrecorded.
func (s *Server) observePaneSession(pid int) {
	rp := s.sup.Get(pid)
	if rp == nil {
		return
	}
	snap := rp.Snapshot()
	agent := snap.AgentID
	if agent == "" {
		return
	}
	rec, hasRec := s.app.SessionRecord(pid)
	if hasRec && rec.Value != "" && strings.HasPrefix(rec.Source, "report:") {
		return // trusted explicit report already decides the session
	}
	job := rp.ForegroundJob()
	cwd := snap.Cwd

	if s.observedStart == nil {
		s.observedStart = map[int]time.Time{}
	}
	start, ok := session.ProcessStart(job)
	if ok && hasRec && rec.Value != "" && s.observedStart[pid].Equal(start) {
		return // already recorded for this exact process; don't re-walk
	}
	if ok {
		if id, found := session.ObserveProcessSession(agent, cwd, start, job, rp.SessionEnvironment()); found {
			if !hasRec || !strings.HasPrefix(rec.Source, "report:") || rec.Value == "" {
				title := rec.Title
				observed := observedSessionRecord(agent, id, title, job)
				s.app.RecordSession(pid, observed)
				s.ledgerPut(pid, observed)
			}
			s.observedStart[pid] = start
			return
		}
	}
	// cmdline is only a launch-shape fallback: it never overwrites a
	// file-observed or reported current session id.
	if hasRec && rec.Value != "" {
		return
	}
	if id, ok2 := session.ObserveCmdline(job); ok2 {
		source := "observed:cmdline"
		if spec, native := nativeResumeAgents[agent]; native {
			source = spec.Source
		}
		s.app.RecordSession(pid, tui.AgentRecord{Agent: agent, Kind: "id", Value: id, Title: rec.Title, Source: source})
		s.ledgerPut(pid, tui.AgentRecord{Agent: agent, Kind: "id", Value: id, Title: rec.Title, Source: source})
		if ok {
			s.observedStart[pid] = start
		}
	}
}

// --- restore plan ----------------------------------------------------------

func (s *Server) buildResumeCommand(agent, kind, value string) (string, bool) {
	return session.ResumeCommand(s.cfg, agent, kind, value)
}

// runAgentRestore builds and optionally executes the restore plan. Call
// on the app loop. It always shows the result toast (SPEC G3).
func (s *Server) runAgentRestore(dryRun bool) api.AgentRestoreResult {
	s.observeAgentSessions()
	var actions []api.AgentRestoreActionInfo
	running := 0
	stt := s.app.State()
	for _, ws := range stt.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.Order() {
				v := s.resolvePaneSession(pid)
				if rp := s.sup.Get(pid); rp != nil && rp.Snapshot().AgentID != "" {
					running++
					// Never double-launch a live agent; surface the
					// per-pane skip with its spec reason.
					reason := "agent already running"
					agent := v.agent
					if agent == "" {
						agent = rp.Snapshot().AgentID
					}
					actions = append(actions, api.AgentRestoreActionInfo{
						PaneID: fmtPaneID(pid), Agent: agent,
						Status: "skipped", Reason: &reason,
					})
					continue
				}
				if !v.terminal && !v.ledger {
					continue // no session association: not a restore target
				}
				if v.agent == "" {
					continue
				}
				if !v.safe {
					reason := "no resumable session found"
					actions = append(actions, api.AgentRestoreActionInfo{
						PaneID: fmtPaneID(pid), Agent: v.agent,
						Status: "skipped", Reason: &reason,
					})
					continue
				}
				cmd, ok := s.buildResumeCommand(v.agent, v.kind, v.value)
				if !ok {
					reason := "no resumable session found"
					actions = append(actions, api.AgentRestoreActionInfo{
						PaneID: fmtPaneID(pid), Agent: v.agent,
						Status: "skipped", Reason: &reason,
					})
					continue
				}
				act := api.AgentRestoreActionInfo{
					PaneID: fmtPaneID(pid), Agent: v.agent,
					Command: &cmd,
				}
				if dryRun {
					act.Status = "would_launch"
				} else {
					act.Status = "launched"
					s.typeResume(pid, cmd)
				}
				actions = append(actions, act)
			}
		}
	}
	s.showRestoreToast(actions, running, dryRun)
	return api.AgentRestoreResult{
		Type:    "agent_restore",
		DryRun:  dryRun,
		Actions: actions,
	}
}

// agentRestore is the API handler for `agent.restore`.
func (s *Server) agentRestore(id string, p api.AgentRestoreParams) api.Response {
	return api.Success(id, s.runAgentRestore(p.DryRun))
}

// typeResume types the resume command into the pane, then submits it
// after a short delay so the shell is ready (SPEC: submit delay).
func (s *Server) typeResume(pid int, cmd string) {
	s.sup.Input(pid, []byte(cmd))
	time.AfterFunc(resumeSubmitDelay, func() {
		s.app.Do(func() { s.sup.Input(pid, []byte("\r")) })
	})
}

// showRestoreToast reports the restore outcome (SPEC G3 empty-state
// wording): distinguish "nothing pending" from "already running",
// dry-run from real execution.
func (s *Server) showRestoreToast(actions []api.AgentRestoreActionInfo, running int, dryRun bool) {
	// "agent already running" rows are informational skips, not restore
	// targets — the toast counts pending/launchable work only (SPEC G3
	// empty-state wording).
	pending := actions[:0:0]
	for _, a := range actions {
		if a.Reason != nil && *a.Reason == "agent already running" {
			continue
		}
		pending = append(pending, a)
	}
	var body string
	if len(pending) == 0 {
		body = "no pending restore"
		if running > 0 {
			body = fmt.Sprintf("no pending restore, %d already running", running)
		}
	} else {
		launched, skipped := 0, 0
		for _, a := range pending {
			switch a.Status {
			case "skipped":
				skipped++
			default:
				launched++
			}
		}
		if dryRun {
			body = fmt.Sprintf("would launch %d, skipped %d", launched, skipped)
		} else {
			body = fmt.Sprintf("launched %d, skipped %d", launched, skipped)
		}
	}
	s.app.ShowToast("agent restore", body, "info", -1)
}

// --- restart warnings --------------------------------------------------------

// agentSessionWarnings lists agent panes that would fail closed on
// restart because no safe session id is recorded (SPEC G3 fail-closed
// restart warning).
func (s *Server) agentSessionWarnings() []api.AgentSessionWarningInfo {
	s.observeAgentSessions()
	var out []api.AgentSessionWarningInfo
	stt := s.app.State()
	for wi, ws := range stt.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.Order() {
				rp := s.sup.Get(pid)
				detected := ""
				if rp != nil {
					detected = rp.Snapshot().AgentID
				}
				v := s.resolvePaneSession(pid)
				isAgentPane := v.terminal || v.ledger || detected != ""
				if !isAgentPane || v.safe {
					continue
				}
				label := paneLabelOf(stt, pid)
				w := api.AgentSessionWarningInfo{
					WorkspaceNumber: wi + 1,
					WorkspaceLabel:  ws.Name,
					PaneLabel:       label,
					Agent:           v.agent,
					Cwd:             s.paneCwdOf(ws, pid),
					Reason:          v.reason,
				}
				if v.title != "" {
					w.Title = &v.title
				}
				out = append(out, w)
			}
		}
	}
	return out
}

func paneLabelOf(stt *model.AppState, pid int) string {
	if l, ok := stt.PaneLabels[pid]; ok && l != "" {
		return l
	}
	return fmtPaneID(pid)
}

// paneCwdOf resolves the pane's working directory: runtime cwd first,
// then the workspace's directory.
func (s *Server) paneCwdOf(ws *model.Workspace, pid int) string {
	if rp := s.sup.Get(pid); rp != nil {
		if c := rp.Snapshot().Cwd; c != "" {
			return c
		}
	}
	return ws.Cwd
}

// scheduleAutoResume runs the agent restore plan once, restore_delay_ms
// after startup, when [session] resume_agents_on_restore is on.
func (s *Server) scheduleAutoResume() {
	if !s.cfg.ResumeAgentsOnRestore() && !s.cfg.LegacyRestoreEnabled() {
		return
	}
	delay := time.Duration(s.cfg.RestoreDelay()) * time.Millisecond
	time.AfterFunc(delay, func() {
		s.app.Do(func() { s.runAgentRestore(false) })
	})
}
