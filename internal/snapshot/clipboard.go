package snapshot

import (
	"encoding/base64"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/kazuph/ikesu/internal/childenv"
)

func Copy(text string, env func(string) string, look func(string) (string, error), tty io.Writer) error {
	if runtime.GOOS == "darwin" && env("SSH_CONNECTION") == "" && env("SSH_CLIENT") == "" && env("SSH_TTY") == "" {
		if copyCommand(text, "/usr/bin/pbcopy") == nil {
			return nil
		}
	}
	if env("WAYLAND_DISPLAY") != "" {
		if path, err := look("wl-copy"); err == nil {
			if err := copyCommand(text, path); err == nil {
				return nil
			}
		}
	}
	if env("DISPLAY") != "" {
		if path, err := look("xclip"); err == nil {
			if err := copyCommand(text, path, "-selection", "clipboard"); err == nil {
				return nil
			}
		}
	}
	if tty == nil {
		return fmt.Errorf("no clipboard command or controlling terminal available")
	}
	_, err := fmt.Fprintf(tty, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(text)))
	return err
}

func copyCommand(text, path string, args ...string) error {
	cmd := childenv.Command(path, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
