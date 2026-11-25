package snapshot

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyOSC52Fallback(t *testing.T) {
	t.Parallel()
	env := func(key string) string {
		if key == "SSH_TTY" {
			return "/dev/pts/1"
		}
		return ""
	}
	look := func(name string) (string, error) { return "", errors.New("not found") }
	var tty bytes.Buffer
	if err := Copy("hello", env, look, &tty); err != nil || tty.String() != "\x1b]52;c;aGVsbG8=\a" {
		t.Fatalf("OSC 52 = %q, error = %v", tty.String(), err)
	}
	if err := Copy("hello", env, look, nil); err == nil {
		t.Fatal("no clipboard and no terminal: expected error")
	}
}

func TestClipboardCommandScrubsPaneEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clipboard")
	output := filepath.Join(dir, "copied")
	script := "#!/bin/sh\n[ -z \"$IKESU_PANE_ID\" ] || exit 1\nexec /bin/cat >\"$1\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IKESU_PANE_ID", "p777")
	if err := copyCommand("fixture", path, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "fixture" {
		t.Fatalf("clipboard command received %q, err=%v", data, err)
	}
}
