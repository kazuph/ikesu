package notify

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/machine"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Desktop dependencies are replaceable so verification never touches the
// user's notification service or audio output.
type Desktop struct {
	Run              func(string, ...string) error
	LookPath         func(string) (string, error)
	Executable       string
	TerminalBundleID string
	ConfigPath       string
}

func (d Desktop) Deliver(n endpoint.Notification, c config.Config, session, machineID string) error {
	run := d.Run
	if run == nil {
		run = func(name string, args ...string) error { return exec.Command(name, args...).Start() }
	}
	lookup := d.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	if n.SystemToast && runtime.GOOS == "darwin" {
		if tn, err := lookup("terminal-notifier"); err == nil {
			exe := d.Executable
			if exe == "" {
				exe, _ = os.Executable()
			}
			words := []string{exe, "--session", session}
			if machineID != "" && machineID != "local" {
				words = append(words, "--machine", machineID)
			}
			words = append(words, "pane", "focus", n.TargetPaneID)
			args := []string{"-title", n.Title, "-message", n.Body, "-execute", machine.Command(words) + " >/dev/null 2>&1"}
			bundle := d.TerminalBundleID
			if bundle == "" {
				bundle = terminalBundle()
			}
			if bundle != "" {
				args = append(args, "-activate", bundle)
			}
			if err := run(tn, args...); err != nil {
				return err
			}
		} else {
			if err := run("/usr/bin/osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, n.Body, n.Title)); err != nil {
				return err
			}
		}
	}
	path, enabled := SoundPath(c, n.Agent, n.Kind)
	if !enabled || os.Getenv("GTX_DISABLE_SOUND") != "" {
		return nil
	}
	if path == "" {
		return fmt.Errorf("unsupported: default %s sound asset is not available", n.Kind)
	}
	if !filepath.IsAbs(path) {
		base := d.ConfigPath
		if base == "" {
			base = config.Path()
		}
		path = filepath.Join(filepath.Dir(base), path)
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("unsupported: sound playback on %s", runtime.GOOS)
	}
	return run("/usr/bin/afplay", path)
}
func SoundPath(c config.Config, agent, kind string) (string, bool) {
	if kind != "finished" && kind != "needs_attention" {
		return "", false
	}
	enabled := c.UI.Sound.Enabled == nil || *c.UI.Sound.Enabled
	if agent == "droid" {
		enabled = false
	}
	switch c.UI.Sound.Agents[agent] {
	case "on":
		enabled = true
	case "off":
		enabled = false
	}
	path := c.UI.Sound.Path
	if kind == "finished" && c.UI.Sound.DonePath != "" {
		path = c.UI.Sound.DonePath
	}
	if kind == "needs_attention" && c.UI.Sound.RequestPath != "" {
		path = c.UI.Sound.RequestPath
	}
	return path, enabled
}
func terminalBundle() string {
	if id := os.Getenv("__CFBundleIdentifier"); id != "" {
		return id
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "apple_terminal":
		return "com.apple.Terminal"
	case "iterm.app":
		return "com.googlecode.iterm2"
	case "ghostty":
		return "com.mitchellh.ghostty"
	case "wezterm":
		return "com.github.wez.wezterm"
	}
	return ""
}
