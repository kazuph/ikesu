package tui

import (
	"os"
	"strconv"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) openJobLog(index int) {
	if index < 0 || index >= len(a.jobRows) {
		return
	}
	j := a.jobRows[index]
	caller, err := strconv.Atoi(strings.TrimPrefix(j.CallerPane, "p"))
	if err != nil {
		return
	}
	wi, _ := a.st.FindPane(caller)
	if wi < 0 || j.LogPath == "" {
		return
	}
	if info, err := os.Stat(j.LogPath); err != nil || !info.Mode().IsRegular() {
		return
	}
	if a.st.Active != wi || a.focusedPane() != caller {
		a.exec(model.CmdFocusPane{Pane: caller})
		return
	}
	w := a.st.ActiveWorkspace()
	if w == nil {
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
