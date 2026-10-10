package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/render"
	"io"
)

func (a *App) hostThemeReport(report uv.Event) {
	if !a.sidebarConfig.Theme.AutoSwitch {
		return
	}
	var dark bool
	switch r := report.(type) {
	case uv.DarkColorSchemeEvent:
		dark = true
	case uv.LightColorSchemeEvent:
		dark = false
	case uv.BackgroundColorEvent:
		if r.Color == nil {
			return
		}
		dark = r.IsDark()
	default:
		return
	}
	c := a.sidebarConfig
	base := c.Theme.Name
	if base == "" {
		base = "catppuccin"
	}
	pairs := map[string][2]string{"catppuccin": {"catppuccin", "catppuccin-latte"}, "catppuccin-latte": {"catppuccin", "catppuccin-latte"}, "tokyo-night": {"tokyo-night", "tokyo-night-day"}, "tokyo-night-day": {"tokyo-night", "tokyo-night-day"}, "gruvbox": {"gruvbox", "gruvbox-light"}, "gruvbox-light": {"gruvbox", "gruvbox-light"}, "one-dark": {"one-dark", "one-light"}, "one-light": {"one-dark", "one-light"}, "solarized": {"solarized", "solarized-light"}, "solarized-light": {"solarized", "solarized-light"}}
	name := base
	if pair, ok := pairs[base]; ok {
		if dark {
			name = pair[0]
		} else {
			name = pair[1]
		}
	}
	if dark && c.Theme.DarkName != "" {
		name = c.Theme.DarkName
	}
	if !dark && c.Theme.LightName != "" {
		name = c.Theme.LightName
	}
	if config.ValidateTheme(name) != nil {
		return
	}
	c.Theme.ResolvedName = name
	a.sidebarConfig = c
	p := render.CustomPalette(name, c.Theme.Custom)
	a.panePalette = &p
	a.dirty = true
}
func (a *App) StartOnboarding() {
	if a.sidebarConfig.Onboarding != nil && !*a.sidebarConfig.Onboarding {
		return
	}
	a.dialog = &uiDialog{onboarding: true, d: render.Dialog{Title: "gtx", Detail: "terminal workspace manager for coding agents", Width: 64, CenterTerminal: true, Confirm: "continue", Lines: []string{"this is a mouse-first terminal.", "click the sidebar to switch workspaces, drag pane", "borders to resize, right-click for context menus.", "", a.prefixSpec() + " enters prefix mode · ? shows keybinds and settings", "next: start managing persistent terminal workspaces"}}, run: func(a *App) {
		if err := config.SaveOnboarding(false); err != nil {
			a.SetConfigWarning(err.Error())
			return
		}
		value := false
		a.sidebarConfig.Onboarding = &value
		a.dialog = nil
		a.workspaceNavigate = true
		a.navigateSelected = a.st.Active
		a.dirty = true
		if a.configChanged != nil {
			a.configChanged(a.sidebarConfig)
		}
	}}
	a.dirty = true
}
func (a *App) prefixSpec() string {
	if s := a.sidebarConfig.Keys.Prefix; s != "" {
		return s
	}
	return "ctrl+b"
}
func (a *App) BeginHostTheme() {
	if a.sidebarConfig.Theme.AutoSwitch {
		_, _ = io.WriteString(a.out, "\x1b[?2031h\x1b[?996n\x1b]11;?\x1b\\")
	}
}
