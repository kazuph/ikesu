package server

import (
	"strings"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// keyNames mirrors the ultraviolet stringKeyType table subset the API
// accepts for pane.send_keys / pane.send_input.
var keyNames = map[string]rune{
	"enter":     uv.KeyEnter,
	"return":    uv.KeyEnter,
	"tab":       uv.KeyTab,
	"backspace": uv.KeyBackspace,
	"escape":    uv.KeyEscape,
	"esc":       uv.KeyEscape,
	"space":     uv.KeySpace,
	"up":        uv.KeyUp,
	"down":      uv.KeyDown,
	"left":      uv.KeyLeft,
	"right":     uv.KeyRight,
	"insert":    uv.KeyInsert,
	"delete":    uv.KeyDelete,
	"pgup":      uv.KeyPgUp,
	"pgdown":    uv.KeyPgDown,
	"home":      uv.KeyHome,
	"end":       uv.KeyEnd,
	"f1":        uv.KeyF1,
	"f2":        uv.KeyF2,
	"f3":        uv.KeyF3,
	"f4":        uv.KeyF4,
	"f5":        uv.KeyF5,
	"f6":        uv.KeyF6,
	"f7":        uv.KeyF7,
	"f8":        uv.KeyF8,
	"f9":        uv.KeyF9,
	"f10":       uv.KeyF10,
	"f11":       uv.KeyF11,
	"f12":       uv.KeyF12,
	"plus":      '+',
}

// parseKeyName converts a CLI key spec like "enter", "ctrl+c", "C-c", or a
// single printable character into a KeyPressEvent. Returns false when the
// spec is not a supported key.
func parseKeyName(spec string) (uv.KeyPressEvent, bool) {
	s := strings.TrimSpace(spec)
	s = normalizeKeyAlias(s)
	var mod uv.KeyMod
	var code rune
	var text string
	parts := strings.Split(s, "+")
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "ctrl", "control":
			mod |= uv.ModCtrl
		case "alt", "option":
			mod |= uv.ModAlt
		case "shift":
			mod |= uv.ModShift
		default:
			if c, ok := keyNames[strings.ToLower(part)]; ok {
				code = c
			} else if utf8.RuneCountInString(part) == 1 {
				code, _ = utf8.DecodeRuneInString(part)
			} else {
				return uv.KeyPressEvent{}, false
			}
		}
	}
	if code == 0 {
		return uv.KeyPressEvent{}, false
	}
	if mod&^(uv.ModShift) == 0 && utf8.ValidRune(code) && code < 0x10000 {
		if r := rune(code); isPrintableASCII(r) || code > 0x7f {
			text = string(code)
		}
	}
	return uv.KeyPressEvent{Code: code, Mod: mod, Text: text}, true
}

// normalizeKeyAlias maps the CLI spellings herdr accepts ("C-c", "+") onto
// modifier-combo form.
func normalizeKeyAlias(s string) string {
	switch s {
	case "+":
		return "plus"
	}
	if len(s) == 3 && (s[0] == 'C' || s[0] == 'c' || s[0] == 'M' || s[0] == 'S') && s[1] == '-' {
		prefix := map[byte]string{'C': "ctrl", 'c': "ctrl", 'M': "alt", 'S': "shift"}[s[0]]
		return prefix + "+" + s[2:]
	}
	return s
}

func isPrintableASCII(r rune) bool { return r >= 0x20 && r < 0x7f }
