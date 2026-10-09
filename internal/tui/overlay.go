package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/model"
)

func (a *App) OpenOverlay(target int, cwd string, argv []string, env map[string]string) (int, error) {
	if a.ModalBusy() {
		return 0, fmt.Errorf("ui_busy")
	}
	wi, ti := a.st.FindPane(target)
	if wi < 0 || wi != a.st.Active || a.st.Workspaces[wi].ActiveTab != ti {
		return 0, fmt.Errorf("overlay target is not in the active tab")
	}
	a.lastSpawnError = nil
	a.exec(model.CmdOpenOverlay{TargetPaneID: target, Cwd: cwd, Argv: argv, Env: env})
	if a.lastSpawnError != nil {
		err := a.lastSpawnError
		a.exec(model.CmdCloseOverlay{})
		return 0, err
	}
	return a.st.Overlay.PaneID, nil
}
func (a *App) ModalBusy() bool {
	return a.settings != nil || a.cm != nil || a.dialog != nil || a.menu != nil || a.help != nil
}
