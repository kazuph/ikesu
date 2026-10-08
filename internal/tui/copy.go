package tui

// Copy mode (SPEC G6): the action bar's copy button cycles
// normal → in-layout copy → fullscreen copy → normal. The state
// machine lands with the copy-mode section; until then the button
// is inert but already owns its hit area.

// copyCycle advances the copy-mode cycle. Filled in with the copy
// mode implementation (SPEC G6); kept as a no-op hook so the action
// bar wiring is complete.
func (a *App) copyCycle() {}
