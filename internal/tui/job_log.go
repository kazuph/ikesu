package tui

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) openJobLog(index int) {
	if index < 0 || index >= len(a.jobRows) {
		return
	}
	j := a.jobRows[index]
	w := a.st.ActiveWorkspace()
	if w == nil {
		return
	}
	if j.LogPath == "" {
		a.ShowToast("job log unavailable", "the job has no saved log path", "error", -1)
		return
	}
	// The observed viewer echoes ordinary keys, follows output, and exits on Ctrl-C.
	size := api.PopupSize{Percent: popupDefaultPercent}
	a.lastSpawnError = nil
	a.exec(model.CmdOpenPopup{WorkspaceID: w.ID, Cwd: j.Cwd,
		Argv:  []string{"/usr/bin/tail", "-n", "+1", "-f", j.LogPath},
		Width: size, Height: size, Title: "job log: " + j.Label})
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdClosePopup{})
		a.ShowToast("job log unavailable", err.Error(), "error", -1)
	}
}
