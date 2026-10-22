package config

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"strconv"
	"strings"
)

type Chord struct {
	Key             uv.Key
	Prefix, Indexed bool
}

var chordKeyNames = map[string]rune{"return": uv.KeyEnter, "bs": uv.KeyBackspace, "period": '.', "slash": '/', "backslash": '\\', "quote": '\'', "semicolon": ';', "colon": ':', "esc": uv.KeyEscape, "escape": uv.KeyEscape, "enter": uv.KeyEnter, "tab": uv.KeyTab, "backspace": uv.KeyBackspace, "left": uv.KeyLeft, "right": uv.KeyRight, "up": uv.KeyUp, "down": uv.KeyDown, "space": ' ', "minus": '-', "comma": ',', "ampersand": '&', "plus": '+', "backtick": '`', "percent": '%', "double_quote": '"'}

func ParseChord(spec string) (Chord, error) {
	var c Chord
	rest := strings.ToLower(strings.TrimSpace(spec))
	for {
		token, tail, ok := strings.Cut(rest, "+")
		if !ok {
			break
		}
		switch token {
		case "prefix":
			c.Prefix = true
		case "ctrl", "control":
			c.Key.Mod |= uv.ModCtrl
		case "alt", "option", "meta":
			c.Key.Mod |= uv.ModAlt
		case "shift":
			c.Key.Mod |= uv.ModShift
		case "cmd", "super", "command":
			c.Key.Mod |= uv.ModSuper
		case "hyper":
			c.Key.Mod |= uv.ModHyper
		default:
			return c, fmt.Errorf("unknown key modifier %q", token)
		}
		rest = tail
	}
	if rest == "1..9" {
		c.Indexed = true
		c.Key.Code = '1'
		return c, nil
	}

	if code, ok := chordKeyNames[rest]; ok {
		c.Key.Code = code
		return c, nil
	}
	if strings.HasPrefix(rest, "f") {
		if n, err := strconv.Atoi(rest[1:]); err == nil && n >= 1 && n <= 24 {
			c.Key.Code = uv.KeyF1 + rune(n-1)
			return c, nil
		}
	}
	runes := []rune(rest)
	if len(runes) != 1 {
		return c, fmt.Errorf("invalid key %q", spec)
	}
	c.Key.Code = runes[0]
	raw := []rune(strings.TrimSpace(spec[strings.LastIndex(spec, "+")+1:]))
	if len(raw) == 1 && raw[0] >= 'A' && raw[0] <= 'Z' {
		c.Key.Mod |= uv.ModShift
	}
	return c, nil
}
func (c Chord) Match(k uv.Key, prefix bool) bool {
	if c.Prefix != prefix {
		return false
	}
	if !c.Indexed {
		// Kitty reports both the physical key and its shifted character. Either
		// can be explicitly configured; the alternate must not replace the base.
		if c.matchCode(k.Code, k.Mod) {
			return true
		}
		return k.Mod.Contains(uv.ModShift) && k.ShiftedCode != 0 && c.matchCode(k.ShiftedCode, k.Mod)
	}
	code, mod := c.IndexCode(k), k.Mod
	if code != k.Code {
		mod |= uv.ModShift
	}
	return mod == c.Key.Mod && code >= '1' && code <= '9'
}

func (c Chord) matchCode(code rune, mod uv.KeyMod) bool {
	if code >= 'A' && code <= 'Z' {
		code += 'a' - 'A'
		mod |= uv.ModShift
	}
	if code == c.Key.Code && code > ' ' && code <= '~' && !(code >= 'a' && code <= 'z') && !(code >= '0' && code <= '9') {
		// Terminals may retain Shift when reporting the resulting punctuation.
		mod = (mod &^ uv.ModShift) | (c.Key.Mod & uv.ModShift)
	}
	return mod == c.Key.Mod && code == c.Key.Code
}

// IndexCode identifies the physical digit when Shift is reported as a symbol.
func (c Chord) IndexCode(k uv.Key) rune {
	code := k.Code
	if k.Mod.Contains(uv.ModShift) && k.ShiftedCode != 0 {
		code = k.ShiftedCode
	}
	if c.Key.Mod.Contains(uv.ModShift) {
		for i, symbol := range "!@#$%^&*(" {
			if code == symbol {
				return rune('1' + i)
			}
		}
	}
	return code
}

// Defaults come from the 0.2.24 commented configuration, with the existing
// ikesu copy/history extensions retained.
var ActionDefaults = map[string]string{
	"swap_pane_left": "prefix+shift+h", "swap_pane_down": "prefix+shift+j", "swap_pane_up": "prefix+shift+k", "swap_pane_right": "prefix+shift+l",
	"help": "prefix+?", "settings": "prefix+s", "detach": "prefix+q", "reload_config": "prefix+shift+r", "open_notification_target": "prefix+o", "workspace_picker": "prefix+w", "goto": "prefix+g", "new_workspace": "prefix+shift+n", "new_worktree": "prefix+shift+g", "open_worktree": "", "remove_worktree": "", "rename_workspace": "prefix+shift+w", "close_workspace": "prefix+shift+d", "previous_workspace": "", "next_workspace": "", "previous_agent": "", "next_agent": "", "focus_agent": "", "remote_image_paste": "ctrl+v", "new_tab": "prefix+c", "rename_tab": "prefix+shift+t", "previous_tab": "prefix+p", "next_tab": "prefix+n", "switch_tab": "prefix+1..9", "switch_workspace": "", "close_tab": "prefix+shift+x", "rename_pane": "prefix+shift+p", "edit_scrollback": "prefix+e", "focus_pane_left": "prefix+h", "focus_pane_down": "prefix+j", "focus_pane_up": "prefix+k", "focus_pane_right": "prefix+l", "cycle_pane_next": "prefix+tab", "cycle_pane_previous": "prefix+shift+tab", "last_pane": "", "split_vertical": "prefix+v", "split_horizontal": "prefix+minus", "close_pane": "prefix+x", "zoom": "prefix+z", "fullscreen": "", "resize_mode": "prefix+r", "toggle_sidebar": "prefix+b", "toggle_right_sidebar": "prefix+shift+b", "navigate_workspace_up": "up", "navigate_workspace_down": "down", "navigate_pane_left": "h", "navigate_pane_down": "j", "navigate_pane_up": "k", "navigate_pane_right": "l", "copy_mode": "prefix+[", "focus_history_back": "", "focus_history_forward": "",
}

func (c Config) ActionBindings(name string) Bindings {
	if b, ok := c.Keys.Actions[name]; ok {
		return b
	}
	switch name {
	case "copy_mode":
		if c.Keys.CopyMode != nil {
			return c.Keys.CopyMode
		}
	case "split_vertical":
		if c.Keys.SplitVertical != nil {
			return c.Keys.SplitVertical
		}
	case "split_horizontal":
		if c.Keys.SplitHorizontal != nil {
			return c.Keys.SplitHorizontal
		}
	case "rename_pane":
		if c.Keys.RenamePane != nil {
			return c.Keys.RenamePane
		}
	case "edit_scrollback":
		if c.Keys.EditScrollback != nil {
			return c.Keys.EditScrollback
		}
	case "focus_history_back":
		if c.Keys.FocusHistoryBack != nil {
			return c.Keys.FocusHistoryBack
		}
	case "focus_history_forward":
		if c.Keys.FocusHistoryForward != nil {
			return c.Keys.FocusHistoryForward
		}
	case "open_notification_target":
		if c.Keys.OpenNotificationTarget != "" {
			return Bindings{c.Keys.OpenNotificationTarget}
		}
	}
	return Bindings{ActionDefaults[name]}
}
