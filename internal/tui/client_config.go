package tui

import "github.com/kazuph/goterrace/internal/config"

func (a *App) editConfiguration(edit config.Edit, done func(config.Config, error)) {
	if a.configEdit != nil {
		a.configEdit(edit, done)
		return
	}
	c, err := edit.ApplyTo(a.sidebarConfig)
	done(c, err)
}

func (v *Viewer) SetConfigEditor(editor func(config.Edit, func(config.Config, error))) {
	v.app.configEdit = editor
}

func (v *Viewer) ConfigurationResult(a *App, done func(config.Config, error), c config.Config, err error) {
	v.with(a, func(*App) { done(c, err) })
}
