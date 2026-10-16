package runtime

import "github.com/charmbracelet/x/ansi"

// OwnsEscape reports whether the focused program uses interactive terminal modes.
func (p *Pane) OwnsEscape() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.altScreen || p.appCursorKeys || p.mouseTracking ||
		p.emu.ModeSet(ansi.ModeFocusEvent) || p.emu.KbdExplicit()
}
