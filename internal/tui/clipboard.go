package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// SSH copies travel to the viewing terminal. Local macOS copies use the
// pasteboard when available; an unavailable pasteboard falls back to OSC 52.
func (a *App) writeClipboard(text string) error {
	if runtime.GOOS == "darwin" && os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_CLIENT") == "" && os.Getenv("SSH_TTY") == "" {
		cmd := exec.Command("/usr/bin/pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return nil
		}
	}
	_, err := fmt.Fprintf(a.out, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}
