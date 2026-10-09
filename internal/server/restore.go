package server

// Exact per-pane agent session restore (SPEC G3): observation of the
// session id a pane's agent is actually running (explicit report wins,
// then session-file match, then cmdline resume-shape), the pane-session
// ledger, the agent.restore plan, and the missing-session warnings used
// by the restart confirmation dialog. No path ever falls back to
// "--last", "resume --last", or a cwd/mtime-latest guess.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/platform"
	"github.com/kazuph/goterrace/internal/session"
	"github.com/kazuph/goterrace/internal/tui"
)

// sessionFileWindow is how far apart a session file's timestamp and the
// agent process's start may be before the match stops counting (SPEC G3:
// 120 seconds, exactly one candidate).
const sessionFileWindow = 120 * time.Second

// resumeSubmitDelay separates typing the resume command from the Enter
// keypress so the receiving shell has finished its prompt draw.
const resumeSubmitDelay = 150 * time.Millisecond

// builtinRestoreTemplates are the default per-agent restore command
// templates; each must contain {session_id}. Only agents with a known
// resume verb get a builtin — anything else needs
// [agent_restore.templates] or [agent_start.commands] and otherwise
// fails closed.
var builtinRestoreTemplates = map[string]string{
	"claude": "claude --resume {session_id}",
	"codex":  "codex resume {session_id}",
	"devin":  "devin --resume {session_id}",
	"grok":   "grok --resume {session_id}",
	"qwen":   "qwen --resume {session_id}",
	"agy":    "agy --conversation {session_id}",
}

// --- ledger --------------------------------------------------------------

// ledgerEntry returns the ledger row for a pane in this session. When
// the DB file does not exist no ledger rows can exist either; avoid
// opening the store so reads stay free of the engine's memory cost.
func (s *Server) ledgerEntry(pid int) (dispatch.PaneSession, bool) {
	if !dispatch.DBExists() {
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
		Session:     s.session,
		PaneID:      pid,
		TerminalID:  fmtPaneID(pid),
		WorkspaceID: wsID,
		TabID:       tabID,
		Cwd:         cwd,
		Agent:       rec.Agent,
		SessionKind: rec.Kind,
		SessionID:   rec.Value,
		Title:       rec.Title,
		Source:      rec.Source,
		ObservedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// ledgerDel drops the ledger row for a permanently closed pane.
func (s *Server) ledgerDel(pid int) {
	delete(s.observedStart, pid)
	if !dispatch.DBExists() {
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
	start, ok := procStartOf(job)
	if ok && hasRec && rec.Value != "" && s.observedStart[pid].Equal(start) {
		return // already recorded for this exact process; don't re-walk
	}
	if ok {
		if id, found := observeSessionFile(agent, cwd, start); found {
			if !hasRec || !strings.HasPrefix(rec.Source, "report:") || rec.Value == "" {
				title := rec.Title
				s.app.RecordSession(pid, tui.AgentRecord{Agent: agent, Kind: "id", Value: id, Title: title, Source: "observed:file"})
				s.ledgerPut(pid, tui.AgentRecord{Agent: agent, Kind: "id", Value: id, Title: title, Source: "observed:file"})
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
	if id, ok2 := observeCmdline(job); ok2 {
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

// procStartOf returns the start time of the foreground job's leader.
func procStartOf(job platform.ForegroundJob) (time.Time, bool) {
	pid := job.ProcessGroupID
	if pid <= 0 && len(job.Processes) > 0 {
		pid = job.Processes[0].PID
	}
	if pid <= 0 {
		return time.Time{}, false
	}
	return platform.ProcStart(pid)
}

// observeSessionFile finds the agent's session file written near the
// process start, restricted to the pane's cwd. Exactly one candidate
// within the window is required; zero or many means "not determined".
func observeSessionFile(agent, cwd string, start time.Time) (string, bool) {
	if cwd == "" {
		return "", false
	}
	switch agent {
	case "claude":
		return observeClaudeSessionFile(cwd, start)
	case "codex":
		return observeCodexSessionFile(cwd, start)
	}
	return "", false
}

// jsonStringField extracts `"key":"value"` from a JSON line, naive but
// sufficient for the well-formed headers agent CLIs write.
func jsonStringField(line, key string) string {
	k := strings.Index(line, `"`+key+`"`)
	if k < 0 {
		return ""
	}
	rest := line[k+len(key)+2:]
	i := strings.IndexByte(rest, '"')
	if i < 0 {
		return ""
	}
	rest = rest[i+1:]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// payloadField extracts key from the `"payload"` object of a
// session_meta line — not from the top level, so an outer field with the
// same name can never be mistaken for it.
func payloadField(line, key string) string {
	i := strings.Index(line, `"payload"`)
	if i < 0 {
		return ""
	}
	return jsonStringField(line[i:], key)
}

// parseSessionTS parses an RFC3339 timestamp from a session record.
func parseSessionTS(v string) (time.Time, bool) {
	ts, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		if ts, err = time.Parse("2006-01-02T15:04:05.000Z", v); err != nil {
			return time.Time{}, false
		}
	}
	return ts, true
}

// headLines calls fn for each of the first n lines of path, stopping
// early when fn returns false.
func headLines(path string, n int, fn func(line string) bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 4<<20))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n > 0 && sc.Scan() {
		n--
		if !fn(sc.Text()) {
			return
		}
	}
}

// observeClaudeSessionFile scans ~/.claude/projects/**/*.jsonl for the
// unique file whose first 64 lines record this cwd and a timestamp
// within the window of the agent process's start (SPEC G3). The project
// folder name is not trusted — distinct cwds slugify to the same dir —
// so the recorded cwd field decides, and the id is the recorded
// sessionId, never the file name.
func observeClaudeSessionFile(cwd string, start time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".claude", "projects")
	// A session file's recorded timestamp precedes its last write, so a
	// file untouched since before the window cannot qualify; checking
	// mtime first keeps the scan cheap.
	cutoff := start.Add(-sessionFileWindow)
	found := ""
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		var id, fcwd string
		var ts time.Time
		headLines(path, 64, func(line string) bool {
			if id == "" {
				id = jsonStringField(line, "sessionId")
			}
			if fcwd == "" {
				fcwd = jsonStringField(line, "cwd")
			}
			if ts.IsZero() {
				if v := jsonStringField(line, "timestamp"); v != "" {
					if t, ok := parseSessionTS(v); ok {
						ts = t
					}
				}
			}
			return id == "" || fcwd == "" || ts.IsZero()
		})
		if id == "" || fcwd != cwd || ts.IsZero() || absDur(ts.Sub(start)) > sessionFileWindow {
			return nil
		}
		count++
		found = id
		return nil
	})
	if count != 1 {
		return "", false
	}
	return found, true
}

// codexSessionMeta reads a rollout file's session_meta record (the first
// line carrying one) and returns payload.id (or payload.session_id),
// payload.cwd, and payload.timestamp.
func codexSessionMeta(path string) (id, cwd string, ts time.Time, ok bool) {
	headLines(path, 8, func(line string) bool {
		if !strings.Contains(line, "session_meta") {
			return true
		}
		id = payloadField(line, "id")
		if id == "" {
			id = payloadField(line, "session_id")
		}
		cwd = payloadField(line, "cwd")
		ts, _ = parseSessionTS(payloadField(line, "timestamp"))
		ok = true
		return false
	})
	return id, cwd, ts, ok && id != "" && !ts.IsZero()
}

// observeCodexSessionFile scans the day folders sessions/YYYY/MM/DD
// around the process start for the unique rollout whose session_meta
// records this cwd and a timestamp within the window (SPEC G3). A meta
// record without payload.cwd never counts.
func observeCodexSessionFile(cwd string, start time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".codex", "sessions")
	found := ""
	count := 0
	for _, day := range []time.Time{start.AddDate(0, 0, -1), start, start.AddDate(0, 0, 1)} {
		dir := filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02"))
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			name := e.Name()
			if e.IsDir() || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			id, fcwd, ts, ok := codexSessionMeta(filepath.Join(dir, name))
			if !ok || fcwd != cwd || absDur(ts.Sub(start)) > sessionFileWindow {
				continue
			}
			count++
			found = id
		}
	}
	if count != 1 {
		return "", false
	}
	return found, true
}

// observeCmdline reads the resume-shape argv of the pane's running
// processes: `X --resume <id>`, `X --resume=<id>`, `X resume <id>`. It is
// a launch-shape fallback only — the caller never lets it overwrite a
// file-observed or reported id. All candidate processes must agree on one
// id or the observation is ambiguous.
func observeCmdline(job platform.ForegroundJob) (string, bool) {
	found := ""
	for _, pr := range job.Processes {
		if id, native, ok := observeNativeResume(pr); native {
			if !ok {
				return "", false
			}
			if id != "" {
				if found != "" && found != id {
					return "", false
				}
				found = id
			}
			continue
		}
		for i, arg := range pr.Argv {
			var v string
			switch {
			case arg == "--resume" && i+1 < len(pr.Argv):
				v = pr.Argv[i+1]
			case strings.HasPrefix(arg, "--resume="):
				v = strings.TrimPrefix(arg, "--resume=")
			case arg == "resume" && i > 0 && i+1 < len(pr.Argv):
				// codex-style `… resume <id>`; argv[i-1] is the binary.
				v = pr.Argv[i+1]
			}
			if v == "" {
				continue
			}
			if found == "" {
				found = v
			} else if found != v {
				return "", false
			}
		}
	}
	return found, found != ""
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// --- restore plan ----------------------------------------------------------

// resumeArgsFor is the per-agent argv suffix appended to a configured
// [agent_start.commands] argv to resume a session.
func resumeArgsFor(agent string) []string {
	if spec, native := nativeResumeAgents[agent]; native {
		return []string{spec.Flag}
	}
	if agent == "codex" {
		return []string{"resume"}
	}
	return []string{"--resume"}
}

// shellJoin quotes argv for typing into a shell prompt.
func shellJoin(argv []string) string {
	var b strings.Builder
	for i, a := range argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@%_+=:,./-~", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildResumeCommand renders the exact resume command line for a pane.
// Returns ok=false when no safe command can be formed — the pane stays a
// shell rather than resuming the wrong conversation.
func (s *Server) buildResumeCommand(agent, kind, value string) (string, bool) {
	if _, native := nativeResumeAgents[agent]; native && (kind != "id" || !session.SafeSessionID(value)) {
		return "", false
	}
	if argv, ok := s.cfg.AgentStartCommand(agent); ok {
		args := append(argv, resumeArgsFor(agent)...)
		args = append(args, value)
		return shellJoin(args), true
	}
	tmpl := s.cfg.RestoreTemplate(agent)
	if tmpl == "" {
		tmpl = builtinRestoreTemplates[agent]
	}
	if tmpl == "" || !strings.Contains(tmpl, "{session_id}") {
		return "", false
	}
	// SPEC G3 fail-closed: a template carrying a standalone --last token
	// can never produce an exact-session command.
	for _, tok := range strings.Fields(tmpl) {
		if tok == "--last" {
			return "", false
		}
	}
	return strings.ReplaceAll(tmpl, "{session_id}", value), true
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
	if !s.cfg.ResumeAgentsOnRestore() {
		return
	}
	delay := time.Duration(s.cfg.RestoreDelay()) * time.Millisecond
	time.AfterFunc(delay, func() {
		s.app.Do(func() { s.runAgentRestore(false) })
	})
}
