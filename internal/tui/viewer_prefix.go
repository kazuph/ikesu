package tui

// The remote clipboard action consumes a prefix key on the client, so its
// viewer must leave prefix mode before the asynchronous paste returns.
func (v *Viewer) EndPrefix(a *App) { v.with(a, func(ui *App) { ui.prefix = false; ui.dirty = true }) }
