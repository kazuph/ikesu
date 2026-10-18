package vt

import "github.com/charmbracelet/x/ansi"

// DcsHandler is a function that handles a DCS escape sequence.
type DcsHandler func(params ansi.Params, data []byte) bool

// CsiHandler is a function that handles a CSI escape sequence.
type CsiHandler func(params ansi.Params) bool

// OscHandler is a function that handles an OSC escape sequence.
type OscHandler func(data []byte) bool

// ApcHandler is a function that handles an APC escape sequence.
type ApcHandler func(data []byte) bool

// SosHandler is a function that handles an SOS escape sequence.
type SosHandler func(data []byte) bool

// PmHandler is a function that handles a PM escape sequence.
type PmHandler func(data []byte) bool

// EscHandler is a function that handles an ESC escape sequence.
type EscHandler func() bool

// CcHandler is a function that handles a control character.
type CcHandler func() bool

// handlers contains the terminal's escape sequence handlers.
type handlers struct {
	ccHandlers  [1 << 8][]CcHandler
	dcsHandlers map[int][]DcsHandler
	csiHandlers map[int][]CsiHandler
	oscHandlers map[int][]OscHandler
	escHandler  map[int][]EscHandler
	apcHandlers []ApcHandler
	sosHandlers []SosHandler
	pmHandlers  []PmHandler
}

// RegisterDcsHandler registers a DCS escape sequence handler.
func (h *handlers) RegisterDcsHandler(cmd int, handler DcsHandler) {
	if h.dcsHandlers == nil {
		h.dcsHandlers = make(map[int][]DcsHandler)
	}
	h.dcsHandlers[cmd] = append(h.dcsHandlers[cmd], handler)
}

// RegisterCsiHandler registers a CSI escape sequence handler.
func (h *handlers) RegisterCsiHandler(cmd int, handler CsiHandler) {
	if h.csiHandlers == nil {
		h.csiHandlers = make(map[int][]CsiHandler)
	}
	h.csiHandlers[cmd] = append(h.csiHandlers[cmd], handler)
}

// RegisterOscHandler registers an OSC escape sequence handler.
func (h *handlers) RegisterOscHandler(cmd int, handler OscHandler) {
	if h.oscHandlers == nil {
		h.oscHandlers = make(map[int][]OscHandler)
	}
	h.oscHandlers[cmd] = append(h.oscHandlers[cmd], handler)
}

// RegisterApcHandler registers an APC escape sequence handler.
func (h *handlers) RegisterApcHandler(handler ApcHandler) {
	h.apcHandlers = append(h.apcHandlers, handler)
}

// RegisterSosHandler registers an SOS escape sequence handler.
func (h *handlers) RegisterSosHandler(handler SosHandler) {
	h.sosHandlers = append(h.sosHandlers, handler)
}

// RegisterPmHandler registers a PM escape sequence handler.
func (h *handlers) RegisterPmHandler(handler PmHandler) {
	h.pmHandlers = append(h.pmHandlers, handler)
}

// RegisterEscHandler registers an ESC escape sequence handler.
func (h *handlers) RegisterEscHandler(cmd int, handler EscHandler) {
	if h.escHandler == nil {
		h.escHandler = make(map[int][]EscHandler)
	}
	h.escHandler[cmd] = append(h.escHandler[cmd], handler)
}

// registerCcHandler registers a control character handler.
func (h *handlers) registerCcHandler(r byte, handler CcHandler) {
	h.ccHandlers[r] = append(h.ccHandlers[r], handler)
}

// handleCc handles a control character.
// It returns true if the control character was handled.
func (h *handlers) handleCc(r byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	for i := len(h.ccHandlers[r]) - 1; i >= 0; i-- {
		if h.ccHandlers[r][i]() {
			return true
		}
	}
	return false
}

// handleDcs handles a DCS escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleDcs(cmd ansi.Cmd, params ansi.Params, data []byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	if handlers, ok := h.dcsHandlers[int(cmd)]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](params, data) {
				return true
			}
		}
	}
	return false
}

// handleCsi handles a CSI escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleCsi(cmd ansi.Cmd, params ansi.Params) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	if handlers, ok := h.csiHandlers[int(cmd)]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](params) {
				return true
			}
		}
	}
	return false
}

// handleOsc handles an OSC escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleOsc(cmd int, data []byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	if handlers, ok := h.oscHandlers[cmd]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i](data) {
				return true
			}
		}
	}
	return false
}

// handleApc handles an APC escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleApc(data []byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	for i := len(h.apcHandlers) - 1; i >= 0; i-- {
		if h.apcHandlers[i](data) {
			return true
		}
	}
	return false
}

// handleSos handles an SOS escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleSos(data []byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	for i := len(h.sosHandlers) - 1; i >= 0; i-- {
		if h.sosHandlers[i](data) {
			return true
		}
	}
	return false
}

// handlePm handles a PM escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handlePm(data []byte) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	for i := len(h.pmHandlers) - 1; i >= 0; i-- {
		if h.pmHandlers[i](data) {
			return true
		}
	}
	return false
}

// handleEsc handles an ESC escape sequence.
// It returns true if the sequence was handled.
func (h *handlers) handleEsc(cmd int) bool {
	// Reverse iterate over the handlers so that the last registered handler
	// is the first to be called.
	if handlers, ok := h.escHandler[cmd]; ok {
		for i := len(handlers) - 1; i >= 0; i-- {
			if handlers[i]() {
				return true
			}
		}
	}
	return false
}

// registerDefaultHandlers registers the default escape sequence handlers.
func (e *Emulator) registerDefaultHandlers() {
	e.registerDefaultCcHandlers()
	e.registerDefaultCsiHandlers()
	e.registerDefaultEscHandlers()
	e.registerDefaultOscHandlers()
}

// registerDefaultCcHandlers registers the default control character handlers.
func (e *Emulator) registerDefaultCcHandlers() {
	for i := byte(ansi.NUL); i <= ansi.US; i++ {
		switch i {
		case ansi.NUL: // Null [ansi.NUL]
			// Ignored
			e.registerCcHandler(i, func() bool {
				return true
			})
		case ansi.BEL: // Bell [ansi.BEL]
			e.registerCcHandler(i, func() bool {
				if e.cb.Bell != nil {
					e.cb.Bell()
				}
				return true
			})
		case ansi.BS: // Backspace [ansi.BS]
			e.registerCcHandler(i, func() bool {
				e.backspace()
				return true
			})
		case ansi.HT: // Horizontal Tab [ansi.HT]
			e.registerCcHandler(i, func() bool {
				e.nextTab(1)
				return true
			})
		case ansi.LF, ansi.VT, ansi.FF:
			// Line Feed [ansi.LF]
			// Vertical Tab [ansi.VT]
			// Form Feed [ansi.FF]
			e.registerCcHandler(i, func() bool {
				e.linefeed()
				return true
			})
		case ansi.CR: // Carriage Return [ansi.CR]
			e.registerCcHandler(i, func() bool {
				e.carriageReturn()
				return true
			})
		}
	}

	for i := byte(ansi.PAD); i <= byte(ansi.APC); i++ {
		switch i {
		case ansi.HTS: // Horizontal Tab Set [ansi.HTS]
			e.registerCcHandler(i, func() bool {
				e.horizontalTabSet()
				return true
			})
		case ansi.RI: // Reverse Index [ansi.RI]
			e.registerCcHandler(i, func() bool {
				e.reverseIndex()
				return true
			})
		case ansi.SO: // Shift Out [ansi.SO]
			e.registerCcHandler(i, func() bool {
				e.gl = 1
				return true
			})
		case ansi.SI: // Shift In [ansi.SI]
			e.registerCcHandler(i, func() bool {
				e.gl = 0
				return true
			})
		case ansi.IND: // Index [ansi.IND]
			e.registerCcHandler(i, func() bool {
				e.index()
				return true
			})
		case ansi.SS2: // Single Shift 2 [ansi.SS2]
			e.registerCcHandler(i, func() bool {
				e.gsingle = 2
				return true
			})
		case ansi.SS3: // Single Shift 3 [ansi.SS3]
			e.registerCcHandler(i, func() bool {
				e.gsingle = 3
				return true
			})
		}
	}
}

// registerDefaultOscHandlers registers the default OSC escape sequence handlers.
func (e *Emulator) registerDefaultOscHandlers() {
	for _, cmd := range []int{
		0, // Set window title and icon name
		1, // Set icon name
		2, // Set window title
	} {
		e.RegisterOscHandler(cmd, func(data []byte) bool {
			e.handleTitle(cmd, data)
			return true
		})
	}

	e.RegisterOscHandler(7, func(data []byte) bool {
		// Report the shell current working directory
		// [ansi.NotifyWorkingDirectory].
		e.handleWorkingDirectory(7, data)
		return true
	})

	e.RegisterOscHandler(8, func(data []byte) bool {
		// Set/Query Hyperlink [ansi.SetHyperlink]
		e.handleHyperlink(8, data)
		return true
	})

	for _, cmd := range []int{
		10,  // Set/Query foreground color
		11,  // Set/Query background color
		12,  // Set/Query cursor color
		110, // Reset foreground color
		111, // Reset background color
		112, // Reset cursor color
	} {
		e.RegisterOscHandler(cmd, func(data []byte) bool {
			e.handleDefaultColor(cmd, data)
			return true
		})
	}
}

// registerDefaultEscHandlers registers the default ESC escape sequence handlers.
func (e *Emulator) registerDefaultEscHandlers() {
	e.RegisterEscHandler('=', func() bool {
		// Keypad Application Mode [ansi.DECKPAM]
		e.setMode(ansi.ModeNumericKeypad, ansi.ModeSet)
		return true
	})

	e.RegisterEscHandler('>', func() bool {
		// Keypad Numeric Mode [ansi.DECKPNM]
		e.setMode(ansi.ModeNumericKeypad, ansi.ModeReset)
		return true
	})

	e.RegisterEscHandler('7', func() bool {
		// Save Cursor [ansi.DECSC]
		e.scr.SaveCursor()
		return true
	})

	e.RegisterEscHandler('8', func() bool {
		// Restore Cursor [ansi.DECRC]
		e.scr.RestoreCursor()
		return true
	})

	for _, cmd := range []int{
		ansi.Command(0, '(', 'A'), // UK G0
		ansi.Command(0, ')', 'A'), // UK G1
		ansi.Command(0, '*', 'A'), // UK G2
		ansi.Command(0, '+', 'A'), // UK G3
		ansi.Command(0, '(', 'B'), // USASCII G0
		ansi.Command(0, ')', 'B'), // USASCII G1
		ansi.Command(0, '*', 'B'), // USASCII G2
		ansi.Command(0, '+', 'B'), // USASCII G3
		ansi.Command(0, '(', '0'), // Special G0
		ansi.Command(0, ')', '0'), // Special G1
		ansi.Command(0, '*', '0'), // Special G2
		ansi.Command(0, '+', '0'), // Special G3
	} {
		e.RegisterEscHandler(cmd, func() bool {
			// Select Character Set [ansi.SCS]
			c := ansi.Cmd(cmd)
			set := c.Intermediate() - '('
			switch c.Final() {
			case 'A': // UK Character Set
				e.charsets[set] = UK
			case 'B': // USASCII Character Set
				e.charsets[set] = nil // USASCII is the default
			case '0': // Special Drawing Character Set
				e.charsets[set] = SpecialDrawing
			default:
				return false
			}
			return true
		})
	}

	e.RegisterEscHandler('D', func() bool {
		// Index [ansi.IND]
		e.index()
		return true
	})

	e.RegisterEscHandler('H', func() bool {
		// Horizontal Tab Set [ansi.HTS]
		e.horizontalTabSet()
		return true
	})

	e.RegisterEscHandler('M', func() bool {
		// Reverse Index [ansi.RI]
		e.reverseIndex()
		return true
	})

	e.RegisterEscHandler('c', func() bool {
		// Reset Initial State [ansi.RIS]
		e.fullReset()
		return true
	})

	e.RegisterEscHandler('n', func() bool {
		// Locking Shift G2 [ansi.LS2]
		e.gl = 2
		return true
	})

	e.RegisterEscHandler('o', func() bool {
		// Locking Shift G3 [ansi.LS3]
		e.gl = 3
		return true
	})

	e.RegisterEscHandler('|', func() bool {
		// Locking Shift 3 Right [ansi.LS3R]
		e.gr = 3
		return true
	})

	e.RegisterEscHandler('}', func() bool {
		// Locking Shift 2 Right [ansi.LS2R]
		e.gr = 2
		return true
	})

	e.RegisterEscHandler('~', func() bool {
		// Locking Shift 1 Right [ansi.LS1R]
		e.gr = 1
		return true
	})
}
