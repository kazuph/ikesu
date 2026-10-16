package input

import (
	"fmt"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// EncodeKey translates the host event into the protocol requested by the pane.
// The local path in the reference does not enable all-key or release delivery.
func EncodeKey(k uv.Key, raw []byte, flags int, explicit, cursor, endpoint, release bool) []byte {
	all := endpoint && flags&ansi.KittyReportAllKeysAsEscapeCodes != 0
	events := flags&ansi.KittyReportEventTypes != 0
	modifier := k.Code >= uv.KeyLeftShift && k.Code <= uv.KeyIsoLevel5Shift
	if modifier || release && (!endpoint || !events || !all && k.Code < uv.KeyExtended && k.Code != uv.KeyEnter && k.Code != uv.KeyTab && k.Code != uv.KeyEscape && k.Code != uv.KeyBackspace) {
		return nil
	}
	mod := int(k.Mod & (uv.ModShift | uv.ModAlt | uv.ModCtrl | uv.ModHyper | uv.ModCapsLock | uv.ModNumLock))
	if k.Mod&uv.ModSuper != 0 {
		mod |= 8
	}
	if k.Mod&uv.ModMeta != 0 {
		mod |= 32
	}
	mod++
	event := 1
	if k.IsRepeat {
		event = 2
	}
	if release {
		event = 3
	}
	parameter := fmt.Sprint(mod)
	if events {
		parameter += fmt.Sprintf(":%d", event)
	}
	code := k.Code
	if all {
		if unicode.IsUpper(code) {
			k.ShiftedCode, code = code, unicode.ToLower(code)
			mod = (mod - 1) | 1
			mod++
			parameter = fmt.Sprint(mod)
			if events {
				parameter += fmt.Sprintf(":%d", event)
			}
		}
		// The reference endpoint reports navigation as numeric-keypad keys.
		if value, ok := allKeyNavigation[code]; ok {
			code = value
		}
		if code >= uv.KeyExtended {
			return raw
		}
		key := fmt.Sprint(code)
		if flags&ansi.KittyReportAlternateKeys != 0 && k.ShiftedCode != 0 && k.ShiftedCode != code {
			key += fmt.Sprintf(":%d", k.ShiftedCode)
		}
		text := ""
		if !release && flags&ansi.KittyReportAssociatedKeys != 0 && k.Text != "" {
			var runes []string
			for _, r := range k.Text {
				runes = append(runes, fmt.Sprint(r))
			}
			text = ";" + strings.Join(runes, ":")
		}
		return []byte("\x1b[" + key + ";" + parameter + text + "u")
	}
	if !release && k.Text != "" {
		return []byte(k.Text)
	}
	if suffix, ok := cursorKeySuffix[code]; ok {
		if events || mod != 1 {
			return []byte("\x1b[1;" + parameter + suffix)
		}
		if cursor {
			return []byte("\x1bO" + suffix)
		}
		return []byte("\x1b[" + suffix)
	}
	if number, ok := tildeKeyNumber[code]; ok {
		if release {
			return []byte(fmt.Sprintf("\x1b[%d;%s~", number, parameter))
		}
		if mod != 1 {
			return []byte(fmt.Sprintf("\x1b[%d;%d~", number, mod))
		}
		return []byte(fmt.Sprintf("\x1b[%d~", number))
	}
	if explicit {
		if code >= uv.KeyExtended {
			return raw
		}
		// Enter uses the disambiguated form without the press event suffix.
		if code == uv.KeyEnter && !release {
			parameter = fmt.Sprint(mod)
		}
		if code == uv.KeyEscape && mod == 1 && !events {
			return []byte("\x1b[27u")
		}
		if mod != 1 || release || code == uv.KeyEscape {
			return []byte(fmt.Sprintf("\x1b[%d;%su", code, parameter))
		}
	}
	if code == uv.KeyEnter && k.Mod == uv.ModShift {
		return []byte("\x1b[27;2;13~")
	}
	if code == uv.KeyEscape && k.Mod == 0 {
		return []byte{0x1b}
	}
	if code < uv.KeyExtended && k.Mod&uv.ModCtrl != 0 {

		if code >= 'a' && code <= 'z' {
			return []byte{byte(code - 'a' + 1)}
		}
		if code == ' ' || code == '@' {
			return []byte{0}
		}
		if code >= '[' && code <= '_' {
			return []byte{byte(code - '[' + 27)}
		}
	}
	return raw
}

var cursorKeySuffix = map[rune]string{uv.KeyUp: "A", uv.KeyDown: "B", uv.KeyRight: "C", uv.KeyLeft: "D", uv.KeyHome: "H", uv.KeyEnd: "F"}
var tildeKeyNumber = map[rune]int{uv.KeyInsert: 2, uv.KeyDelete: 3, uv.KeyPgUp: 5, uv.KeyPgDown: 6, uv.KeyF5: 15, uv.KeyF6: 17, uv.KeyF7: 18, uv.KeyF8: 19, uv.KeyF9: 20, uv.KeyF10: 21, uv.KeyF11: 23, uv.KeyF12: 24}
var allKeyNavigation = map[rune]rune{uv.KeyUp: 57419, uv.KeyDown: 57420, uv.KeyLeft: 57417, uv.KeyRight: 57418, uv.KeyPgUp: 57421, uv.KeyPgDown: 57422, uv.KeyHome: 57423, uv.KeyEnd: 57424, uv.KeyInsert: 57425, uv.KeyDelete: 57426}
