package tui

import (
	"fmt"
	"image/color"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/session"
)

// AgentRecord is a pane's persisted agent-session association: which
// agent ran here and the exact session it belongs to. The record is what
// makes per-pane resume exact instead of cwd-guessed (SPEC G3).
type AgentRecord struct {
	Agent  string // canonical agent id (claude, codex, ...)
	Kind   string // "id" | "path"
	Value  string // session id or transcript path
	Title  string
	Source string // "report:<source>" | "gtx:<agent>" | "observed" | "restore"
}

// SetChangeHook registers the callback invoked on the app loop after any
// persistent state change; the server uses it to coalesce session saves.
func (a *App) SetChangeHook(f func()) { a.changeHook = f }

// SetRestartHook registers the restart callback used by the main menu's
// "Restart server…" item; nil keeps the item disabled (local mode).
func (a *App) SetRestartHook(f func()) { a.restartHook = f }

// SetRestoreHook registers the agent-restore callback used by the main
// menu's "Restore agents…" item; nil keeps the item disabled.
func (a *App) SetRestoreHook(f func(dryRun bool)) { a.restoreHook = f }

// SetPaneClosedHook registers the callback invoked when a pane is
// permanently removed so the server can drop its ledger row (SPEC G3).
func (a *App) SetPaneClosedHook(f func(pid int)) { a.paneClosedHook = f }

// SetWarningsHook registers the missing-session warning provider used by
// the restart confirmation dialog (SPEC G3 fail-closed restart warning).
func (a *App) SetWarningsHook(f func() []api.AgentSessionWarningInfo) {
	a.warningsHook = f
}

// changed notifies the persistence hook. Call on the app loop.
func (a *App) changed() {
	if a.changeHook != nil {
		a.changeHook()
	}
}

// RecordSession stores or replaces the pane's agent-session record.
// Call on the app loop.
func (a *App) RecordSession(pid int, rec AgentRecord) {
	if a.sessions == nil {
		a.sessions = map[int]*AgentRecord{}
	}
	a.sessions[pid] = &rec
	a.dirty = true
	a.changed()
}

// ClearSession drops the pane's agent-session record. Call on the app loop.
func (a *App) ClearSession(pid int) {
	if _, ok := a.sessions[pid]; ok {
		delete(a.sessions, pid)
		a.changed()
	}
}

// SessionRecord returns the pane's recorded agent session, if any.
// Call on the app loop.
func (a *App) SessionRecord(pid int) (AgentRecord, bool) {
	if rec, ok := a.sessions[pid]; ok {
		return *rec, true
	}
	return AgentRecord{}, false
}

// SessionRecords returns a copy of every pane's agent-session record.
// Call on the app loop.
func (a *App) SessionRecords() map[int]AgentRecord {
	out := make(map[int]AgentRecord, len(a.sessions))
	for pid, rec := range a.sessions {
		out[pid] = *rec
	}
	return out
}

// RestoreSession replaces the app state with the saved session and spawns
// a shell in each pane's persisted working directory, reusing the saved
// pane numbers (SPEC G3/G5). Call before Run, exactly where
// StartInitialPane would have run. It returns the ids of panes carrying
// an agent record so the caller can schedule exact session resume.
func (a *App) RestoreSession(f *session.File) []int {
	cols, rows := a.st.Cols, a.st.Rows // terminal size is runtime, not saved
	a.st = model.Restore(f)
	a.st.Cols, a.st.Rows = cols, rows
	a.applyUIFlags()
	a.SetSidebarConfig(a.sidebarConfig, false)
	if a.sessions == nil {
		a.sessions = map[int]*AgentRecord{}
	}
	cwds := map[int]string{}
	var agents []int
	f.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		cwds[p.ID] = p.Cwd
		if p.Agent != "" || p.SessionID != "" || p.Title != "" {
			a.sessions[p.ID] = &AgentRecord{
				Agent:  p.Agent,
				Kind:   p.SessionKind,
				Value:  p.SessionID,
				Title:  p.Title,
				Source: "restore",
			}
			agents = append(agents, p.ID)
			if p.Agent != "" {
				if a.reported == nil {
					a.reported = map[int]reportedPane{}
				}
				a.reported[p.ID] = reportedPane{agent: p.Agent}
			}
		}
	})
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				cwd := cwds[pid]
				if cwd == "" || !dirExists(cwd) {
					cwd = ws.Cwd
				}
				if cwd == "" {
					cwd = a.cwd
				}
				r := a.rectForPane(pid)
				if _, err := a.sup.Spawn(pid, cwd, max(r.W, 1), max(r.H, 1)); err != nil && cwd != a.cwd {
					_, _ = a.sup.Spawn(pid, a.cwd, max(r.W, 1), max(r.H, 1))
				}
			}
		}
	}
	a.dirty = true
	return agents
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// openRestartDialog confirms a server restart. When agent panes are
// missing a safe session id the dialog switches to the fail-closed
// warning form, listing each unsafe pane (SPEC G3).
func (a *App) openRestartDialog() {
	d := render.Dialog{Title: "Restart gtx?", Detail: "Restarts the gtx server. The saved session can be restored.", Confirm: "restart", Cancel: "cancel", Width: 76}
	if a.warningsHook != nil {
		if ws := a.warningsHook(); len(ws) > 0 {
			d.Title = "Restart with missing agent sessions?"
			d.Detail = "These AI panes do not have a recorded session id:"
			d.Width = 76
			for i, w := range ws {
				if i >= 10 { // SPEC: the missing-pane list grows at most 10 rows
					break
				}
				d.Lines = append(d.Lines, restartWarningLine(w))
			}
		}
	}
	a.dialog = &uiDialog{
		d: d,
		run: func(a *App) {
			if a.restartHook != nil {
				a.restartHook()
			}
		},
	}
	a.dirty = true
}

// restartWarningLine renders one missing-session warning row in the
// SPEC G3 form: ` space N label pane P agent title=T cwd=C reason=R`,
// truncated with a trailing ellipsis when it would exceed the dialog.
func restartWarningLine(w api.AgentSessionWarningInfo) string {
	ws := w.WorkspaceLabel
	if ws == "" {
		ws = fmt.Sprintf("w%d", w.WorkspaceNumber)
	}
	title := "-"
	if w.Title != nil && *w.Title != "" {
		title = *w.Title
	}
	line := fmt.Sprintf(" space %d %s pane %s %s title=%s cwd=%s reason=%s",
		w.WorkspaceNumber, ws, w.PaneLabel, w.Agent, title, w.Cwd, w.Reason)
	if r := []rune(line); len(r) > 140 {
		line = string(r[:140]) + "…"
	}
	return line
}

// openRestoreDialog confirms a manual agent restore.
func (a *App) openRestoreDialog() {
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Restore agents?", Detail: "Types resume commands into panes with recorded agent sessions.", Confirm: "restore", Cancel: "cancel", Width: 76},
		run: func(a *App) {
			if a.restoreHook != nil {
				a.restoreHook(false)
			}
		},
	}
	a.dirty = true
}

// retainOnExit reports whether a dying pane is kept as a shell-fallback
// pane: it must still carry an effective agent label (detected or
// reported) and a same-agent record with a safe session value — stale
// ledger alone never keeps a plain shell alive (SPEC G3 restorable pane
// retention / inactive shell cleanup boundary).
func (a *App) retainOnExit(pid int) bool {
	rp := a.sup.Get(pid)
	if rp == nil {
		return false
	}
	agent := rp.Snapshot().AgentID
	if agent == "" {
		if r, ok := a.reported[pid]; ok {
			agent = r.agent
		}
	}
	if agent == "" {
		return false
	}
	rec, ok := a.sessions[pid]
	if !ok || rec.Agent != agent {
		return false
	}
	return session.SafeSessionValue(rec.Kind, rec.Value)
}

// respawnShell replaces a retained pane's dead process with a fresh login
// shell in the pane's working directory, keeping the pane id, label, and
// agent-session record so a later restore can resume the exact session.
// Call on the app loop.
func (a *App) respawnShell(pid int) {
	cwd := ""
	if rp := a.sup.Get(pid); rp != nil {
		cwd = rp.SpawnCwd()
	}
	if wsIdx, _ := a.st.FindPane(pid); wsIdx >= 0 && cwd == "" {
		cwd = a.st.Workspaces[wsIdx].Cwd
	}
	if cwd == "" {
		cwd = a.cwd
	}
	a.sup.Kill(pid)
	r := a.rectForPane(pid)
	_, _ = a.sup.Spawn(pid, cwd, max(r.W, 1), max(r.H, 1))
	// The terminal side is now a bare shell: keep the agent label (the
	// pane stays a restorable owner) but drop the stale reported state.
	if r0, ok := a.reported[pid]; ok && r0.agent != "" {
		a.reported[pid] = reportedPane{agent: r0.agent, state: "unknown"}
	}
	a.dirty = true
	a.changed()
}

// --- G2: workspace job dots ---------------------------------------------

// SetJobSource installs the jobs snapshot provider (server mode only):
// jobs newest-first plus the set of job ids whose runner process was
// alive at snapshot time — per SPEC G2 liveness is checked here, not
// during rendering.
func (a *App) SetJobSource(f func() ([]dispatch.Job, map[string]bool)) {
	a.jobSource = f
}

// refreshJobDots re-snapshots jobs at most once per second and folds
// them into per-workspace dots. Meaningful rows only: queued jobs,
// running/cancelling jobs with a live runner, and finished rows younger
// than one minute. Everything else leaves no dot.
func (a *App) refreshJobDots(now time.Time) {
	if a.jobSource == nil || now.Sub(a.jobsAt) < time.Second {
		return
	}
	a.jobsAt = now
	jobs, alive := a.jobSource()
	rows := make([]render.JobRow, 0, len(jobs))
	for _, j := range jobs {
		col := render.Green
		switch j.Status {
		case "queued":
			col = render.Dim
		case "running", "cancelling":
			col = render.Yellow
		default:
			if j.ExitCode != nil && *j.ExitCode != 0 {
				col = render.Red
			}
		}
		rows = append(rows, render.JobRow{ID: j.ID, Label: j.Label, Status: j.Status, Color: col})
	}
	if !reflect.DeepEqual(a.jobRows, rows) {
		a.jobRows = rows
		a.dirty = true
	}
	dots := map[int][]render.JobDot{}
	for _, j := range jobs {
		var col color.Color
		switch j.Status {
		case "queued":
			col = render.Dim
		case "running", "cancelling":
			if !alive[j.ID] {
				continue // dead runner → no dot
			}
			col = render.Yellow
		default: // finished / cancelled / failed rows
			if j.FinishedMs == nil ||
				now.Sub(time.UnixMilli(*j.FinishedMs)) > time.Minute {
				continue
			}
			col = render.Green
			if j.ExitCode != nil && *j.ExitCode != 0 {
				col = render.Red
			}
		}
		ws := a.wsIndexOfPane(j.CallerPane)
		if ws < 0 {
			continue
		}
		dots[ws] = append(dots[ws], render.JobDot{Color: col})
	}
	if !reflect.DeepEqual(a.jobDots, dots) {
		a.jobDots = dots
		a.dirty = true
	}
}

// wsIndexOfPane resolves a "pN" caller-pane id to its workspace index.
func (a *App) wsIndexOfPane(paneRef string) int {
	v := strings.TrimPrefix(strings.TrimSpace(paneRef), "p")
	pid, err := strconv.Atoi(v)
	if err != nil || pid <= 0 {
		return -1
	}
	for i, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, id := range t.Layout.PaneIDs() {
				if id == pid {
					return i
				}
			}
		}
	}
	return -1
}

// TestRefreshJobDots is the server-test entry point into the tick's job
// refresh — it bypasses the once-per-second throttle.
func (a *App) TestRefreshJobDots() {
	a.jobsAt = time.Time{}
	a.refreshJobDots(time.Now())
}

// JobDots exposes the workspace dot snapshot for tests.
func (a *App) JobDots() map[int][]render.JobDot { return a.jobDots }

// Jobs uses the same unfiltered snapshot from which card dots are selected.
func (a *App) Jobs() []render.JobRow { return a.jobRows }
