package kitty

import (
	"github.com/creack/pty"
	"os"
	"testing"
)

func TestKoiImageCapabilityOverSSH(t *testing.T) {
	for _, key := range []string{"TERM_PROGRAM", "TERM", "KITTY_WINDOW_ID", "SSH_CONNECTION", "SSH_TTY", "TMUX", "STY"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("SSH_CONNECTION", "192.0.2.1 1 192.0.2.2 2")
	if !SupportsImages(terminal, terminal) {
		t.Error("SSH-capable Ghostty lost in-band koi images")
	}
	if SupportsTerminal(terminal, terminal, false) {
		t.Error("SSH enabled local file transport")
	}
	t.Setenv("TMUX", "session")
	if SupportsImages(terminal, terminal) {
		t.Error("multiplexer did not force octants")
	}
}
