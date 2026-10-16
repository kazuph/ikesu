package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/input"
)

func (a *App) sendPaneKey(k uv.Key, raw []byte, release bool) {
	if release && (a.dialog != nil || a.menu != nil || a.navigator != nil || a.help != nil || a.settings != nil || a.cm != nil || a.prefix || a.resizeMode || a.workspaceNavigate) {
		return
	}
	pid := a.focusedPane()
	p := a.sup.Get(pid)
	if p == nil {
		return
	}
	flags, _, explicit, legacy, cursor := p.KeyboardModes()
	if legacy {
		if !release {
			a.sup.Input(pid, raw)
		}
		return
	}
	data := input.EncodeKey(k, raw, flags, explicit, cursor, a.serverUI, release)
	if len(data) > 0 {
		a.sup.Input(pid, data)
	}
}

func (a *App) syncHostKeyboard() {
	if !a.serverUI {
		return
	}
	wanted := ansi.KittyDisambiguateEscapeCodes | ansi.KittyReportEventTypes | ansi.KittyReportAlternateKeys
	if p := a.sup.Get(a.focusedPane()); p != nil {
		flags, mok, _, _, _ := p.KeyboardModes()
		if flags&ansi.KittyReportAllKeysAsEscapeCodes != 0 || flags&ansi.KittyReportEventTypes != 0 && mok != 0 {
			wanted = ansi.KittyAllFlags
		}
	}
	if a.hostKbdFlags == 0 {
		a.hostKbdFlags = 7
	}
	if wanted != a.hostKbdFlags {
		fmt.Fprintf(a.out, "\x1b[<1u\x1b[>%du", wanted)
		a.hostKbdFlags = wanted
	}
}
