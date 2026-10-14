package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/session"
)

// AppendSession reuses model.Restore for imported layouts. All shells are
// prepared before publishing the new state; spawn failure closes only the
// prepared panes and leaves every existing pane/state intact.
func (a *App) AppendSession(merged, added *session.File) error {
	prepared := []int{}
	var spawnErr error
	added.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if spawnErr != nil {
			return
		}
		if _, err := a.sup.Spawn(p.ID, p.Cwd, max(a.st.Cols, 1), max(a.st.Rows, 1)); err != nil {
			spawnErr = fmt.Errorf("pane %d: %w", p.ID, err)
			return
		}
		prepared = append(prepared, p.ID)
	})
	if spawnErr != nil {
		for _, id := range prepared {
			a.sup.Kill(id)
		}
		return spawnErr
	}
	a.st.AppendSession(merged, added)
	a.applyUIFlags()
	a.SetSidebarConfig(a.sidebarConfig, false)
	added.EachPane(func(_ *session.Workspace, _ *session.Tab, p *session.Pane) {
		if p.Agent != "" && p.SessionID != "" {
			a.RecordSession(p.ID, AgentRecord{Agent: p.Agent, Kind: p.SessionKind, Value: p.SessionID, Title: p.Title, Source: "restore"})
		}
	})
	a.effect(model.EffSyncLayout{})
	a.dirty = true
	a.changed()
	return nil
}
