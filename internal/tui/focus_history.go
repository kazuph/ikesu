package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) SetPaneKeybinds(c config.Config) {
	a.renameKeys = c.RenamePaneKeys()
	a.editKeys = c.EditScrollbackKeys()
	a.historyBackKeys = c.FocusHistoryBackKeys()
	a.historyForwardKeys = c.FocusHistoryForwardKeys()
}

func (a *App) recordFocus(before, after int) {
	if a.traversingHistory || before == after || after < 0 {
		return
	}
	if len(a.focusHistory) == 0 && before >= 0 {
		a.focusHistory = append(a.focusHistory, before)
	}
	if len(a.focusHistory) > 0 {
		a.focusHistory = a.focusHistory[:a.focusHistoryAt+1]
	}
	a.focusHistory = append(a.focusHistory, after)
	a.focusHistoryAt = len(a.focusHistory) - 1
}

func (a *App) focusHistoryStep(forward bool) {
	delta := -1
	if forward {
		delta = 1
	}
	for i := a.focusHistoryAt + delta; i >= 0 && i < len(a.focusHistory); i += delta {
		pid := a.focusHistory[i]
		if pid == a.focusedPane() {
			continue
		}
		for wi, w := range a.st.Workspaces {
			for ti, tab := range w.Tabs {
				if !tab.Layout.HasPane(pid) {
					continue
				}
				a.traversingHistory = true
				a.exec(model.CmdSelectWorkspace{Index: wi})
				a.exec(model.CmdSelectTab{Workspace: wi, Tab: ti})
				a.exec(model.CmdFocusPane{Pane: pid})
				a.traversingHistory = false
				a.focusHistoryAt = i
				return
			}
		}
	}
}
