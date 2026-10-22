package tui

import "github.com/kazuph/ikesu/internal/config"

func (a *App) toastVisible() bool {
	if a.toast == nil || !a.toast.shown {
		return false
	}
	if a.serverUI {
		if a.toast.target >= 0 && a.sidebarConfig.ToastDelivery() != config.ToastIKESU {
			return false
		}
	}
	return true
}
