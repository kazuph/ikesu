package endpoint

import (
	"os"
	"strings"
)

// Terminal describes the display host, independently of the server host.
type Terminal struct {
	Program string `json:"program,omitempty"`
	Term    string `json:"term,omitempty"`
	Kitty   bool   `json:"kitty,omitempty"`
	Tmux    bool   `json:"tmux,omitempty"`
}

func LocalTerminal() *Terminal {
	_, kitty := os.LookupEnv("KITTY_WINDOW_ID")
	_, tmux := os.LookupEnv("TMUX")
	return &Terminal{Program: os.Getenv("TERM_PROGRAM"), Term: os.Getenv("TERM"), Kitty: kitty, Tmux: tmux}
}

// NotificationBytes follows the terminal protocols specified in FIX-1.
func (t *Terminal) NotificationBytes(title, body string) string {
	if t == nil {
		return ""
	}
	kitty := t.Kitty || strings.Contains(t.Term, "xterm-kitty")
	osc9 := t.Program == "ghostty" || t.Program == "iTerm.app" || t.Program == "WezTerm" || strings.Contains(t.Term, "xterm-ghostty") || strings.Contains(t.Term, "wezterm")
	sequence := ""
	if kitty {
		if body == "" {
			sequence = "\x1b]99;;" + title + "\x1b\\"
		} else {
			sequence = "\x1b]99;i=1:d=0;" + title + "\x1b\\\x1b]99;i=1:p=body;" + body + "\x1b\\"
		}
	} else if osc9 {
		if body != "" {
			title += ": " + body
		}
		sequence = "\x1b]9;" + title + "\x1b\\"
	}
	if sequence != "" && t.Tmux {
		sequence = "\x1bPtmux;" + strings.ReplaceAll(sequence, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return sequence
}
