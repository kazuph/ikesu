package notify

import (
	"errors"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopNotificationClickAndSoundCommands(t *testing.T) {
	t.Setenv("GTX_DISABLE_SOUND", "")
	c, err := config.Decode([]byte("[ui.sound]\npath='all.mp3'\ndone_path='done.mp3'\nrequest_path='request.mp3'\n[ui.sound.agents]\ndroid='on'\nclaude='off'\n"))
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	d := Desktop{Executable: "/tmp/my gtx", TerminalBundleID: "com.mitchellh.ghostty", ConfigPath: filepath.Join(t.TempDir(), "config.toml"), LookPath: func(string) (string, error) { return "/test/terminal-notifier", nil }, Run: func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}}
	n := endpoint.Notification{Title: "2 workspace", Body: "answer", Kind: "finished", Agent: "droid", TargetPaneID: "p12", SystemToast: true}
	if err := d.Deliver(n, c, "other-session", "mini"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(strings.Join(calls[0], " "), "'--machine' 'mini' 'pane' 'focus' 'p12'") || !strings.Contains(strings.Join(calls[0], " "), "-activate com.mitchellh.ghostty") || calls[1][1] != filepath.Join(filepath.Dir(d.ConfigPath), "done.mp3") {
		t.Fatal(calls)
	}
	calls = nil
	n.Kind = "needs_attention"
	n.Agent = "claude"
	if err := d.Deliver(n, c, "local", ""); err != nil || len(calls) != 1 {
		t.Fatal(calls, err)
	}
	calls = nil
	n.Agent = "codex"
	if err := d.Deliver(n, c, "local", ""); err != nil || len(calls) != 2 || calls[1][0] != "/usr/bin/afplay" || calls[1][1] != filepath.Join(filepath.Dir(d.ConfigPath), "request.mp3") {
		t.Fatal("question sound override was not invoked once", calls, err)
	}
	n.Agent = "claude"
	d.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	calls = nil
	if err := d.Deliver(n, c, "local", ""); err != nil || len(calls) != 1 || calls[0][0] != "/usr/bin/osascript" || strings.Contains(strings.Join(calls[0], " "), "pane focus") {
		t.Fatal(calls, err)
	}
}
func TestSoundPathsAgentOverridesAndDefaults(t *testing.T) {
	c, err := config.Decode([]byte("[ui.sound]\npath='all.mp3'\ndone_path='done.mp3'\n[ui.sound.agents]\nclaude='off'\ndroid='on'\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		agent, kind, path string
		enabled           bool
	}{{"codex", "finished", "done.mp3", true}, {"codex", "needs_attention", "all.mp3", true}, {"claude", "finished", "done.mp3", false}, {"droid", "finished", "done.mp3", true}} {
		p, on := SoundPath(c, tc.agent, tc.kind)
		if p != tc.path || on != tc.enabled {
			t.Fatal(tc, p, on)
		}
	}
	if _, on := SoundPath(config.Config{}, "droid", "finished"); on {
		t.Fatal("droid default must be muted")
	}
}
