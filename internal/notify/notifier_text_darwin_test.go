package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
)

// terminal-notifier 3.1 exits 2 for a title or message that begins with "[".
func TestTerminalNotifierEscapesLeadingBracket(t *testing.T) {
	t.Setenv("IKESU_DISABLE_SOUND", "1")
	var got []string
	d := Desktop{
		Executable:       "/tmp/ikesu",
		TerminalBundleID: "-",
		LookPath:         func(name string) (string, error) { return "/test/" + name, nil },
		Run: func(name string, args ...string) error {
			got = args
			return nil
		},
	}
	n := endpoint.Notification{Title: "[ikesu] done", Body: "[ikesu run] exit=0 label=x", SystemToast: true, TargetPaneID: "p1"}
	if err := d.Deliver(n, config.Config{}, "test", ""); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for i := 0; i+1 < len(got); i += 2 {
		values[got[i]] = got[i+1]
	}
	if values["-title"] != `\[ikesu] done` || values["-message"] != `\[ikesu run] exit=0 label=x` {
		t.Fatalf("leading brackets were not escaped: %q", got)
	}
	plain := endpoint.Notification{Title: "ikesu", Body: "a [b]", SystemToast: true, TargetPaneID: "p1"}
	if err := d.Deliver(plain, config.Config{}, "test", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(got); i += 2 {
		values[got[i]] = got[i+1]
	}
	if values["-title"] != "ikesu" || values["-message"] != "a [b]" {
		t.Fatalf("text without a leading bracket changed: %q", got)
	}
}

// A notification command that fails after it started is reported through
// Warn, so a client can show it on its status line instead of writing to the
// terminal underneath the panes.
func TestNotificationCommandFailureGoesToWarn(t *testing.T) {
	t.Setenv("IKESU_DISABLE_SOUND", "1")
	script := filepath.Join(t.TempDir(), "terminal-notifier")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	warned := make(chan error, 1)
	d := Desktop{
		Executable:       "/tmp/ikesu",
		TerminalBundleID: "-",
		LookPath:         func(string) (string, error) { return script, nil },
		Warn:             func(err error) { warned <- err },
	}
	if err := d.Deliver(endpoint.Notification{Title: "t", Body: "b", SystemToast: true, TargetPaneID: "p1"}, config.Config{}, "test", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-warned:
		if !strings.Contains(err.Error(), "terminal-notifier") || !strings.Contains(err.Error(), "exit status 2") {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification command failure was not reported through Warn")
	}
}
