package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
)

func (a *App) openSettings() {
	c := a.sidebarConfig
	a.settings = &render.Settings{Section: a.settingsSection, Theme: c.ThemeName(), Sound: c.SoundEnabled(), Toast: string(c.ToastDelivery())}
	a.settingsSelectCurrent()
	a.dirty = true
}
func (a *App) settingsSelectCurrent() {
	s := a.settings
	s.Selected = 0
	switch s.Section {
	case 0:
		for i, n := range render.ThemeNames {
			if n == s.Theme {
				s.Selected = i
			}
		}
	case 1:
		if !s.Sound {
			s.Selected = 1
		}
	case 2:
		for i, n := range render.ToastNames {
			if n == s.Toast {
				s.Selected = i
			}
		}
	}
}
func (a *App) settingsCount() int {
	switch a.settings.Section {
	case 0:
		return len(render.ThemeNames)
	case 1:
		return 2
	default:
		return len(render.ToastNames)
	}
}
func (a *App) settingsApply() {
	s := a.settings
	var edit config.Edit
	switch s.Section {
	case 0:
		edit = config.Edit{Kind: "theme", Value: render.ThemeNames[s.Selected]}
	case 1:
		edit = config.Edit{Kind: "sound", Enabled: s.Selected == 0}
	case 2:
		edit = config.Edit{Kind: "toast", Value: render.ToastNames[s.Selected]}
	}
	a.editConfiguration(edit, func(c config.Config, err error) {
		if err != nil {
			a.worktreeError(err)
			return
		}
		panePalette := a.panePalette
		if a.configEdit != nil {
			a.setSidebarConfig(c, false)
		} else {
			a.SetSidebarConfig(c, false)
		}
		if a.serverUI {
			// Endpoint pane chrome changes on config reload, not settings apply.
			a.panePalette = panePalette
		}
		if a.configChanged != nil {
			a.configChanged(c)
		}
		s.Theme = c.ThemeName()
		s.Sound = c.SoundEnabled()
		s.Toast = string(c.ToastDelivery())
		if a.settings == s {
			a.settings = nil
		}
		a.dirty = true
	})
}
func (a *App) settingsKey(k uv.Key) {
	s := a.settings
	switch k.Code {
	case uv.KeyEscape:
		a.settings = nil
	case uv.KeyEnter:
		a.settingsApply()
	case uv.KeyTab:
		dir := 1
		if k.Mod.Contains(uv.ModShift) {
			dir = -1
		}
		s.Section = (s.Section + dir + 3) % 3
		a.settingsSelectCurrent()
	case uv.KeyLeft:
		s.Section = (s.Section + 2) % 3
		a.settingsSelectCurrent()
	case uv.KeyRight:
		s.Section = (s.Section + 1) % 3
		a.settingsSelectCurrent()
	case uv.KeyUp:
		s.Selected = max(0, s.Selected-1)
	case uv.KeyDown:
		s.Selected = min(a.settingsCount()-1, s.Selected+1)
	}
	a.settingsSection = s.Section
	if a.settings != nil && s.Section == 0 {
		s.Theme = render.ThemeNames[s.Selected]
	}
	a.dirty = true
}
func (a *App) settingsMouse(ev input.Event) {
	if ev.Kind != input.KindMouseClick {
		return
	}
	m := ev.Mouse
	r := render.SettingsRect(a.st.Cols, a.st.Rows)
	if m.X < r.X || m.X >= r.X+r.W || m.Y < r.Y || m.Y >= r.Y+r.H {
		a.settings = nil
		return
	}
	if m.Y == render.SettingsTabsY(r) {
		for i, offset := range []int{2, 10, 18} {
			if m.X >= r.X+offset && m.X < r.X+offset+6 {
				a.settings.Section = i
				a.settingsSection = i
				a.settingsSelectCurrent()
				return
			}
		}
	}
	offset := render.SettingsChoiceOffset(r, a.settings)
	for i := offset; i < a.settingsCount(); i++ {
		if !render.SettingsChoiceVisible(r, a.settings.Section, i-offset) {
			continue
		}
		if m.Y == render.SettingsChoiceY(r, a.settings.Section, i-offset) && m.X > r.X && m.X < r.X+r.W-1 {
			a.settings.Selected = i
			if a.settings.Section == 0 {
				a.settings.Theme = render.ThemeNames[i]
			}
			return
		}
	}
	apply, close := render.SettingsButtons(r)
	if m.Y == apply.Y && m.X >= apply.X && m.X < apply.X+apply.W {
		a.settingsApply()
	}
	if m.Y == close.Y && m.X >= close.X && m.X < close.X+close.W {
		a.settings = nil
	}
}
func (a *App) SetConfigChanged(hook func(config.Config)) { a.configChanged = hook }
