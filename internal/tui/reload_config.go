package tui

import "github.com/kazuph/goterrace/internal/config"

// ApplyReloadedConfig runs on the app loop and preserves the current view choices.
func (a *App) ApplyReloadedConfig(c config.Config) {
	a.SetConfigWarning("")
	if a.configEdit != nil {
		a.setSidebarConfig(c, false)
	} else {
		a.SetSidebarConfig(c, false)
	}
	if a.configChanged != nil {
		a.configChanged(c)
	}
	a.SetUIFlags(c.ShowTabBar(), c.HideTabBarWhenSingleTab(), c.PaneBorders())
	a.SetKeybinds(c.CopyKeys(), c.SplitVerticalKeys(), c.SplitHorizontalKeys())
	a.SetPaneKeybinds(c)
	a.syncLayout()
	a.changed()
	if a.serverUI && c.Warning() == "" {
		a.SetConfigWarning("reloaded config")
		a.configWarningBanner = true
	} else {
		a.ShowToast("reloaded config", "using config.toml", "info", -1)
	}
}
