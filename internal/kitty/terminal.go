package kitty

import (
	"golang.org/x/sys/unix"
	"os"
	"strings"
)

// SupportsTerminal is the existing direct-image capability check, shared by presenters.
func SupportsTerminal(in, out *os.File, remote bool) bool {
	if remote || in == nil || out == nil {
		return false
	}
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "TMUX", "STY"} {
		if _, present := os.LookupEnv(key); present {
			return false
		}
	}
	if _, err := unix.IoctlGetWinsize(int(in.Fd()), unix.TIOCGWINSZ); err != nil {
		return false
	}
	if _, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ); err != nil {
		return false
	}
	program, terminal := os.Getenv("TERM_PROGRAM"), os.Getenv("TERM")
	_, hasKitty := os.LookupEnv("KITTY_WINDOW_ID")
	return strings.EqualFold(program, "ghostty") || strings.EqualFold(program, "wezterm") || terminal == "xterm-ghostty" || terminal == "xterm-kitty" || terminal == "xterm-wezterm" || hasKitty
}
