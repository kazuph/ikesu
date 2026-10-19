package tui

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/model"
)

type displayState struct {
	visible, hidden, manual, tabs, hideSingle, borders, gaps bool
	size, min, max, normal, mobile                           int
	rightMode                                                string
	rightSize                                                int
	explicitHidden                                           bool
	split                                                    *float64
	density, scope                                           string
}

func captureDisplay(s *model.AppState) displayState {
	return displayState{s.SidebarVisible, s.SidebarCollapsedHidden, s.SidebarManual,
		s.ShowTabBar, s.HideTabBarWhenSingleTab, s.PaneBorders, s.PaneGaps,
		s.SidebarSize, s.SidebarMin, s.SidebarMax, s.SidebarDefault, s.MobileWidthThreshold,
		s.RightSidebarMode, s.RightSidebarSize, s.SidebarHidden, s.SidebarSectionSplit, s.WorkspaceDensity, s.AgentScope}
}

func (d displayState) apply(s *model.AppState) {
	s.SidebarVisible, s.SidebarCollapsedHidden, s.SidebarManual = d.visible, d.hidden, d.manual
	s.ShowTabBar, s.HideTabBarWhenSingleTab, s.PaneBorders, s.PaneGaps = d.tabs, d.hideSingle, d.borders, d.gaps
	s.SidebarSize, s.SidebarMin, s.SidebarMax, s.SidebarDefault = d.size, d.min, d.max, d.normal
	s.MobileWidthThreshold, s.SidebarSectionSplit = d.mobile, d.split
	s.WorkspaceDensity, s.AgentScope = d.density, d.scope
	s.RightSidebarMode, s.RightSidebarSize, s.SidebarHidden = d.rightMode, d.rightSize, d.explicitHidden
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
		ui.applyViewPreferences(preferences)
	})
}

func (a *App) saveViewPreferences() {
	if a.preferenceSave == nil {
		return
	}
	p := endpoint.ViewPreferences{SidebarWidth: a.st.ExpandedSidebarWidth(), SidebarManual: a.st.SidebarManual,
		SidebarCollapsed: !a.st.SidebarVisible, SidebarHidden: a.st.SidebarHidden, RightSidebarMode: a.st.RightSidebarMode}
	if err := a.preferenceSave(p); err != nil {
		a.worktreeError(err)
	}
}

func (v *Viewer) UpdatePresentation(a *App, c *config.Config, preferences *endpoint.ViewPreferences) {
	v.Configure(a, c, preferences, v.app.preferenceSave)
	v.with(a, func(ui *App) {
		if !ui.windowBlurred {
			ui.syncLayout()
		}
		ui.dirty = true
	})
}

func (v *Viewer) ConfigError(a *App, reason string) {
	v.with(a, func(ui *App) { ui.SetConfigWarning(reason); ui.dirty = true })
}

func (a *App) applyViewPreferences(p *endpoint.ViewPreferences) {
	if p == nil {
		return
	}
	lo, hi, _ := a.st.SidebarBounds()
	a.st.SidebarSize = max(lo, min(hi, p.SidebarWidth))
	a.st.SidebarManual = p.SidebarManual
	a.st.SidebarVisible = !p.SidebarCollapsed
	a.st.SidebarHidden = p.SidebarHidden
	a.st.RightSidebarMode = p.RightSidebarMode
}

func (a *App) configureStandalonePreferences() error {
	path := endpoint.ViewPreferencesPath(api.DefaultSession)
	preferences, err := endpoint.LoadViewPreferences(path)
	if err != nil {
		return err
	}
	a.applyViewPreferences(preferences)
	a.preferenceSave = func(p endpoint.ViewPreferences) error { return endpoint.SaveViewPreferences(path, p) }
	return nil
}
