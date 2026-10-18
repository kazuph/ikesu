package tui

import "github.com/kazuph/goterrace/internal/config"

func (v *Viewer) ShowToast(a *App, title, body, kind string, target int) {
	v.with(a, func(ui *App) { ui.ShowToast(title, body, kind, target) })
}

func (a *App) toastVisible() bool {
	if a.toast == nil || !a.toast.shown {
		return false
	}
	if a.serverUI {
		if !a.st.Mobile() && a.sidebarConfig.UI.Toast.Herdr.Position == "" {
			return false
		}
		if a.toast.target >= 0 && a.sidebarConfig.ToastDelivery() != config.ToastGTX {
			return false
		}
	}
	return true
}
