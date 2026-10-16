package runtime

import "github.com/charmbracelet/x/ansi"

func (p *Pane) KeyboardModes() (flags, mok int, explicit, legacy, cursor bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	flags, mok = p.emu.KeyboardModes()
	return flags, mok, p.emu.KbdExplicit(), p.emu.KeyboardLegacy(), p.appCursorKeys
}

// OwnsEscape reports whether the focused program uses interactive terminal modes.
func (p *Pane) OwnsEscape() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.altScreen || p.appCursorKeys || p.mouseTracking ||
		p.emu.ModeSet(ansi.ModeFocusEvent) || p.emu.KbdExplicit()
}
