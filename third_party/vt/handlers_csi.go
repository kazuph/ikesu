package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"io"
)

func (e *Emulator) registerDefaultCsiHandlers() {
	e.RegisterCsiHandler('@', func(params ansi.Params) bool {
		// Insert Character [ansi.ICH]
		n, _, _ := params.Param(0, 1)
		e.scr.InsertCell(n)
		return true
	})

	e.RegisterCsiHandler('A', func(params ansi.Params) bool {
		// Cursor Up [ansi.CUU]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, -n)
		return true
	})

	e.RegisterCsiHandler('B', func(params ansi.Params) bool {
		// Cursor Down [ansi.CUD]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, n)
		return true
	})

	e.RegisterCsiHandler('C', func(params ansi.Params) bool {
		// Cursor Forward [ansi.CUF]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(n, 0)
		return true
	})

	e.RegisterCsiHandler('D', func(params ansi.Params) bool {
		// Cursor Backward [ansi.CUB]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(-n, 0)
		return true
	})

	e.RegisterCsiHandler('E', func(params ansi.Params) bool {
		// Cursor Next Line [ansi.CNL]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, n)
		e.carriageReturn()
		return true
	})

	e.RegisterCsiHandler('F', func(params ansi.Params) bool {
		// Cursor Previous Line [ansi.CPL]
		n, _, _ := params.Param(0, 1)
		e.moveCursor(0, -n)
		e.carriageReturn()
		return true
	})

	e.RegisterCsiHandler('G', func(params ansi.Params) bool {
		// Cursor Horizontal Absolute [ansi.CHA]
		n, _, _ := params.Param(0, 1)
		_, y := e.scr.CursorPosition()
		e.setCursor(n-1, y)
		return true
	})

	e.RegisterCsiHandler('H', func(params ansi.Params) bool {
		// Cursor Position [ansi.CUP]
		width, height := e.Width(), e.Height()
		row, _, _ := params.Param(0, 1)
		col, _, _ := params.Param(1, 1)
		if row < 1 {
			row = 1
		}
		if col < 1 {
			col = 1
		}
		y := min(height-1, row-1)
		x := min(width-1, col-1)
		e.setCursorPosition(x, y)
		return true
	})

	e.RegisterCsiHandler('I', func(params ansi.Params) bool {
		// Cursor Horizontal Tabulation [ansi.CHT]
		n, _, _ := params.Param(0, 1)
		e.nextTab(n)
		return true
	})

	e.RegisterCsiHandler('J', func(params ansi.Params) bool {
		// Erase in Display [ansi.ED]
		n, _, _ := params.Param(0, 0)
		width, height := e.Width(), e.Height()
		x, y := e.scr.CursorPosition()
		switch n {
		case 0: // Erase screen below (from after cursor position)
			rect1 := uv.Rect(x, y, width, 1)            // cursor to end of line
			rect2 := uv.Rect(0, y+1, width, height-y-1) // next line onwards
			e.scr.FillArea(e.scr.blankCell(), rect1)
			e.scr.FillArea(e.scr.blankCell(), rect2)
		case 1: // Erase screen above (including cursor)
			rect := uv.Rect(0, 0, width, y+1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		case 2: // erase screen
			// Save screen content to scrollback before clearing
			e.scr.ClearWithScrollback()
		case 3: // erase display (including scrollback in some terminals)
			// For ED 3, we clear the screen but also clear scrollback
			// This matches xterm behavior where ESC[3J clears scrollback
			e.scr.Clear()
			if sb := e.scr.Scrollback(); sb != nil {
				sb.Clear()
			}
		default:
			return false
		}
		return true
	})

	e.RegisterCsiHandler('K', func(params ansi.Params) bool {
		// Erase in Line [ansi.EL]
		n, _, _ := params.Param(0, 0)
		// NOTE: Erase Line (EL) erases all character attributes but not cell
		// bg color.
		x, y := e.scr.CursorPosition()
		w := e.scr.Width()

		switch n {
		case 0: // Erase from cursor to end of line
			e.eraseCharacter(w - x)
		case 1: // Erase from start of line to cursor
			rect := uv.Rect(0, y, x+1, 1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		case 2: // Erase entire line
			rect := uv.Rect(0, y, w, 1)
			e.scr.FillArea(e.scr.blankCell(), rect)
		default:
			return false
		}
		return true
	})

	e.RegisterCsiHandler('L', func(params ansi.Params) bool {
		// Insert Line [ansi.IL]
		n, _, _ := params.Param(0, 1)
		if e.scr.InsertLine(n) {
			// Move the cursor to the left margin.
			e.scr.setCursorX(0, true)
		}
		return true
	})

	e.RegisterCsiHandler('M', func(params ansi.Params) bool {
		// Delete Line [ansi.DL]
		n, _, _ := params.Param(0, 1)
		if e.scr.DeleteLine(n) {
			// If the line was deleted successfully, move the cursor to the
			// left.
			// Move the cursor to the left margin.
			e.scr.setCursorX(0, true)
		}
		return true
	})

	e.RegisterCsiHandler('P', func(params ansi.Params) bool {
		// Delete Character [ansi.DCH]
		n, _, _ := params.Param(0, 1)
		e.scr.DeleteCell(n)
		return true
	})

	e.RegisterCsiHandler('S', func(params ansi.Params) bool {
		// Scroll Up [ansi.SU]
		n, _, _ := params.Param(0, 1)
		e.scr.ScrollUp(n)
		return true
	})

	e.RegisterCsiHandler('T', func(params ansi.Params) bool {
		// Scroll Down [ansi.SD]
		n, _, _ := params.Param(0, 1)
		e.scr.ScrollDown(n)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'W'), func(params ansi.Params) bool {
		// Set Tab at Every 8 Columns [ansi.DECST8C]
		if len(params) == 1 && params[0] == 5 {
			e.resetTabStops()
			return true
		}
		return false
	})

	e.RegisterCsiHandler('X', func(params ansi.Params) bool {
		// Erase Character [ansi.ECH]
		n, _, _ := params.Param(0, 1)
		e.eraseCharacter(n)
		return true
	})

	e.RegisterCsiHandler('Z', func(params ansi.Params) bool {
		// Cursor Backward Tabulation [ansi.CBT]
		n, _, _ := params.Param(0, 1)
		e.prevTab(n)
		return true
	})

	e.RegisterCsiHandler('`', func(params ansi.Params) bool {
		// Horizontal Position Absolute [ansi.HPA]
		n, _, _ := params.Param(0, 1)
		width := e.Width()
		_, y := e.scr.CursorPosition()
		e.setCursorPosition(min(width-1, n-1), y)
		return true
	})

	e.RegisterCsiHandler('a', func(params ansi.Params) bool {
		// Horizontal Position Relative [ansi.HPR]
		n, _, _ := params.Param(0, 1)
		width := e.Width()
		x, y := e.scr.CursorPosition()
		e.setCursorPosition(min(width-1, x+n), y)
		return true
	})

	e.RegisterCsiHandler('b', func(params ansi.Params) bool {
		// Repeat Previous Character [ansi.REP]
		n, _, _ := params.Param(0, 1)
		e.repeatPreviousCharacter(n)
		return true
	})

	e.RegisterCsiHandler('c', func(params ansi.Params) bool {
		// Primary Device Attributes [ansi.DA1]
		n, _, _ := params.Param(0, 0)
		if n != 0 {
			return false
		}

		// Do we fully support VT220?
		_, _ = io.WriteString(e.pw, ansi.PrimaryDeviceAttributes(
			62, // VT220
			1,  // 132 columns
			6,  // Selective Erase
			22, // ANSI color
		))
		return true
	})

	e.RegisterCsiHandler(ansi.Command('>', 0, 'c'), func(params ansi.Params) bool {
		// Secondary Device Attributes [ansi.DA2]
		n, _, _ := params.Param(0, 0)
		if n != 0 {
			return false
		}

		// Do we fully support VT220?
		_, _ = io.WriteString(e.pw, ansi.SecondaryDeviceAttributes(
			1,  // VT220
			10, // Version 1.0
			0,  // ROM Cartridge is always zero
		))
		return true
	})

	e.RegisterCsiHandler('d', func(params ansi.Params) bool {
		// Vertical Position Absolute [ansi.VPA]
		n, _, _ := params.Param(0, 1)
		height := e.Height()
		x, _ := e.scr.CursorPosition()
		e.setCursorPosition(x, min(height-1, n-1))
		return true
	})

	e.RegisterCsiHandler('e', func(params ansi.Params) bool {
		// Vertical Position Relative [ansi.VPR]
		n, _, _ := params.Param(0, 1)
		height := e.Height()
		x, y := e.scr.CursorPosition()
		e.setCursorPosition(x, min(height-1, y+n))
		return true
	})

	e.RegisterCsiHandler('f', func(params ansi.Params) bool {
		// Horizontal and Vertical Position [ansi.HVP]
		width, height := e.Width(), e.Height()
		row, _, _ := params.Param(0, 1)
		col, _, _ := params.Param(1, 1)
		y := min(height-1, row-1)
		x := min(width-1, col-1)
		e.setCursor(x, y)
		return true
	})

	e.RegisterCsiHandler('g', func(params ansi.Params) bool {
		// Tab Clear [ansi.TBC]
		value, _, _ := params.Param(0, 0)
		switch value {
		case 0:
			x, _ := e.scr.CursorPosition()
			e.tabstops.Reset(x)
		case 3:
			e.tabstops.Clear()
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler('h', func(params ansi.Params) bool {
		// Set Mode [ansi.SM] - ANSI
		e.handleMode(params, true, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'h'), func(params ansi.Params) bool {
		// Set Mode [ansi.SM] - DEC
		e.handleMode(params, true, false)
		return true
	})

	e.RegisterCsiHandler('l', func(params ansi.Params) bool {
		// Reset Mode [ansi.RM] - ANSI
		e.handleMode(params, false, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'l'), func(params ansi.Params) bool {
		// Reset Mode [ansi.RM] - DEC
		e.handleMode(params, false, false)
		return true
	})

	e.RegisterCsiHandler('m', func(params ansi.Params) bool {
		// Select Graphic Rendition [ansi.SGR]
		e.handleSgr(params)
		return true
	})

	e.RegisterCsiHandler('n', func(params ansi.Params) bool {
		// Device Status Report [ansi.DSR]
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}

		switch n {
		case 5: // Operating Status
			// We're always ready ;)
			// See: https://vt100.net/docs/vt510-rm/DSR-OS.html
			_, _ = io.WriteString(e.pw, ansi.DeviceStatusReport(ansi.ANSIStatusReport(0)))
		case 6: // Cursor Position Report [ansi.CPR]
			x, y := e.scr.CursorPosition()
			_, _ = io.WriteString(e.pw, ansi.CursorPositionReport(y+1, x+1))
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'n'), func(params ansi.Params) bool {
		n, _, ok := params.Param(0, 1)
		if !ok || n == 0 {
			return false
		}

		switch n {
		case 6: // Extended Cursor Position Report [ansi.DECXCPR]
			x, y := e.scr.CursorPosition()
			_, _ = io.WriteString(e.pw, ansi.ExtendedCursorPositionReport(y+1, x+1, 0)) // We don't support page numbers //nolint:errcheck
		default:
			return false
		}

		return true
	})

	e.RegisterCsiHandler(ansi.Command(0, '$', 'p'), func(params ansi.Params) bool {
		// Request Mode [ansi.DECRQM] - ANSI
		e.handleRequestMode(params, true)
		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', '$', 'p'), func(params ansi.Params) bool {
		// Request Mode [ansi.DECRQM] - DEC
		e.handleRequestMode(params, false)
		return true
	})

	e.RegisterCsiHandler(ansi.Command(0, ' ', 'q'), func(params ansi.Params) bool {
		// Set Cursor Style [ansi.DECSCUSR]
		n := 1
		if param, _, ok := params.Param(0, 0); ok && param > n {
			n = param
		}
		blink := n == 0 || n%2 == 1
		style := n / 2
		if !blink {
			style--
		}
		e.scr.setCursorStyle(CursorStyle(style), blink)
		return true
	})

	e.RegisterCsiHandler('r', func(params ansi.Params) bool {
		// Set Top and Bottom Margins [ansi.DECSTBM]
		top, _, _ := params.Param(0, 1)
		if top < 1 {
			top = 1
		}

		height := e.Height()
		bottom, _ := e.parser.Param(1, height)
		if bottom < 1 {
			bottom = height
		}

		if top >= bottom {
			return false
		}

		// Rect is [x, y) which means y is exclusive. So the top margin
		// is the top of the screen minus one.
		e.scr.setVerticalMargins(top-1, bottom)

		// Move the cursor to the top-left of the screen or scroll region
		// depending on [ansi.DECOM].
		e.setCursorPosition(0, 0)
		return true
	})

	e.RegisterCsiHandler('s', func(params ansi.Params) bool {
		// Set Left and Right Margins [ansi.DECSLRM]
		// These conflict with each other. When [ansi.DECSLRM] is set, the we
		// set the left and right margins. Otherwise, we save the cursor
		// position.

		if e.isModeSet(ansi.ModeLeftRightMargin) {
			// Set Left Right Margins [ansi.DECSLRM]
			left, _, _ := params.Param(0, 1)
			if left < 1 {
				left = 1
			}

			width := e.Width()
			right, _, _ := params.Param(1, width)
			if right < 1 {
				right = width
			}

			if left >= right {
				return false
			}

			e.scr.setHorizontalMargins(left-1, right)

			// Move the cursor to the top-left of the screen or scroll region
			// depending on [ansi.DECOM].
			e.setCursorPosition(0, 0)
		} else {
			// Save Current Cursor Position [ansi.SCOSC]
			e.scr.SaveCursor()
		}

		return true
	})

	e.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(params ansi.Params) bool {
		// FixTerm keyboard enhancement query. We report support for no
		// kitty flags: CSI ? 0 u (SPEC G6 Ghostty compatibility). A flags
		// field of 0 is the response we ourselves emit — answering it
		// would loop forever when a canonical-mode tty echoes our own
		// response back as input.
		if len(params) > 0 {
			if v, _, _ := params.Param(0, 0); v == 0 {
				return true // our own response echoed back
			}
		}
		_, _ = io.WriteString(e.pw, "\x1b[?0u")
		return true
	})

	e.registerKeyboardHandlers()
}
