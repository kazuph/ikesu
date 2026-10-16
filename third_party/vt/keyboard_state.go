package vt

import "github.com/charmbracelet/x/ansi"

type keyboardEntry struct {
	Flags  int
	Legacy bool
}

type keyboardTransfer struct {
	Flags           int
	Stack           []keyboardEntry
	ModifyOtherKeys int
	Legacy          bool
}

func (e *Emulator) KeyboardModes() (flags, modifyOtherKeys int) {
	return e.keyboard.Flags, e.keyboard.ModifyOtherKeys
}

// KeyboardLegacy identifies a transferred old-server request with unknown flags.
func (e *Emulator) KeyboardLegacy() bool { return e.keyboard.Legacy }

func (e *Emulator) registerKeyboardHandlers() {
	e.RegisterCsiHandler(ansi.Command('>', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		e.keyboard.Stack = append(e.keyboard.Stack, keyboardEntry{e.keyboard.Flags, e.keyboard.Legacy})
		e.keyboard.Flags, e.keyboard.Legacy = flags, false
		return true
	})
	e.RegisterCsiHandler(ansi.Command('=', 0, 'u'), func(params ansi.Params) bool {
		flags, _, _ := params.Param(0, 0)
		mode, _, _ := params.Param(1, 1)
		switch mode {
		case 1:
			e.keyboard.Flags = flags
		case 2:
			e.keyboard.Flags |= flags
		case 3:
			e.keyboard.Flags &^= flags
		default:
			return false
		}
		e.keyboard.Legacy = false
		return true
	})
	e.RegisterCsiHandler(ansi.Command('<', 0, 'u'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 1)
		for ; n > 0 && len(e.keyboard.Stack) > 0; n-- {
			last := len(e.keyboard.Stack) - 1
			entry := e.keyboard.Stack[last]
			e.keyboard.Flags, e.keyboard.Legacy = entry.Flags, entry.Legacy
			e.keyboard.Stack = e.keyboard.Stack[:last]
		}
		return true
	})
	e.RegisterCsiHandler(ansi.Command('>', 0, 'm'), func(params ansi.Params) bool {
		resource, _, _ := params.Param(0, 0)
		if resource != 4 {
			return false
		}
		mode, _, _ := params.Param(1, 0)
		e.keyboard.ModifyOtherKeys = mode
		return true
	})
}
