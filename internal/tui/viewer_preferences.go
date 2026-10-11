package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/model"
)

type displayState struct {
	visible, hidden, manual, tabs, hideSingle, borders bool
	size, min, max, normal, mobile                     int
	split                                              *float64
	density, scope                                     string
}

func captureDisplay(s *model.AppState) displayState {
	return displayState{s.SidebarVisible, s.SidebarCollapsedHidden, s.SidebarManual,
		s.ShowTabBar, s.HideTabBarWhenSingleTab, s.PaneBorders,
		s.SidebarSize, s.SidebarMin, s.SidebarMax, s.SidebarDefault, s.MobileWidthThreshold,
		s.SidebarSectionSplit, s.WorkspaceDensity, s.AgentScope}
}

func (d displayState) apply(s *model.AppState) {
	s.SidebarVisible, s.SidebarCollapsedHidden, s.SidebarManual = d.visible, d.hidden, d.manual
	s.ShowTabBar, s.HideTabBarWhenSingleTab, s.PaneBorders = d.tabs, d.hideSingle, d.borders
	s.SidebarSize, s.SidebarMin, s.SidebarMax, s.SidebarDefault = d.size, d.min, d.max, d.normal
	s.MobileWidthThreshold, s.SidebarSectionSplit = d.mobile, d.split
	s.WorkspaceDensity, s.AgentScope = d.density, d.scope
}

func (v *Viewer) Configure(a *App, c *config.Config, preferences *endpoint.ViewPreferences, save func(endpoint.ViewPreferences) error) {
	v.with(a, func(ui *App) {
		ui.configChanged = nil
		ui.preferenceSave = save
		if c != nil {
			ui.setSidebarConfig(*c, true)
			ui.SetUIFlags(c.ShowTabBar(), c.HideTabBarWhenSingleTab(), c.PaneBorders())
			ui.SetKeybinds(c.CopyKeys(), c.SplitVerticalKeys(), c.SplitHorizontalKeys())
			ui.SetPaneKeybinds(*c)
		}
		if preferences != nil {
			lo, hi, _ := ui.st.SidebarBounds()
			ui.st.SidebarSize = max(lo, min(hi, preferences.SidebarWidth))
			ui.st.SidebarManual = preferences.SidebarManual
			ui.st.SidebarVisible = !preferences.SidebarCollapsed
			ui.st.SidebarSectionSplit = preferences.SidebarSectionSplit
		}
	})
}

func (a *App) saveViewPreferences() {
	if a.preferenceSave == nil {
		return
	}
	p := endpoint.ViewPreferences{SidebarWidth: a.st.ExpandedSidebarWidth(), SidebarManual: a.st.SidebarManual,
		SidebarCollapsed: !a.st.SidebarVisible, SidebarSectionSplit: a.st.SidebarSectionSplit}
	if err := a.preferenceSave(p); err != nil {
		a.worktreeError(err)
	}
}

func (v *Viewer) ConfigError(a *App, reason string) {
	v.with(a, func(ui *App) { ui.SetConfigWarning(reason); ui.dirty = true })
}
