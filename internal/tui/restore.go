package tui

import (
	"os"

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

// openRestartDialog confirms a server restart.
func (a *App) openRestartDialog() {
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Restart gtx?", Detail: "Restarts the gtx server and restores this session.", Confirm: "restart", Cancel: "cancel"},
		run: func(a *App) {
			if a.restartHook != nil {
				a.restartHook()
			}
		},
	}
	a.dirty = true
}

// openRestoreDialog confirms a manual agent restore.
func (a *App) openRestoreDialog() {
	a.dialog = &uiDialog{
		d: render.Dialog{Title: "Restore agents?", Detail: "Types resume commands into panes with recorded agent sessions.", Confirm: "restore", Cancel: "cancel"},
		run: func(a *App) {
			if a.restoreHook != nil {
				a.restoreHook(false)
			}
		},
	}
	a.dirty = true
}
