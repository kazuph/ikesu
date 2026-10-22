package notify

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/machine"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Desktop dependencies are replaceable so verification never touches the
// user's notification service or audio output.
type Desktop struct {
	Run              func(string, ...string) error
	LookPath         func(string) (string, error)
	Executable       string
	TerminalBundleID string
	ConfigPath       string
	CacheDir         func() (string, error)
	Warn             func(error)
	soundWarning     *sync.Once
}

func (d Desktop) Deliver(n endpoint.Notification, c config.Config, session, machineID string) error {
	run := d.Run
	if run == nil {
		run = func(name string, args ...string) error {
			cmd := childenv.Command(name, args...)
			if err := cmd.Start(); err != nil {
				return err
			}
			go func() {
				if err := cmd.Wait(); err != nil {
					fmt.Fprintln(os.Stderr, "ikesu: notification command:", err)
				}
			}()
			return nil
		}
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
	if n.SystemToast && runtime.GOOS == "linux" {
		if command, err := lookup("notify-send"); err == nil {
			if err := run(command, "--", n.Title, n.Body); err != nil {
				return err
			}
		}
	}
	path, enabled := SoundPath(c, n.Agent, n.Kind)
	if !enabled || os.Getenv("IKESU_DISABLE_SOUND") != "" {
		return nil
	}
	if path == "" {
		var err error
		path, err = d.defaultSound(n.Kind)
		if err != nil {
			d.warnSound(fmt.Errorf("sound cache: %w", err))
			return nil
		}
	}
	if !filepath.IsAbs(path) {
		base := d.ConfigPath
		if base == "" {
			base = config.Path()
		}
		path = filepath.Join(filepath.Dir(base), path)
	}
	switch runtime.GOOS {
	case "darwin":
		return run("/usr/bin/afplay", path)
	case "linux":
		for _, candidate := range []string{"paplay", "pw-play", "ffplay", "mpg123", "mpv"} {
			command, err := lookup(candidate)
			if err != nil {
				continue
			}
			args := []string{path}
			switch candidate {
			case "ffplay":
				args = []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path}
			case "mpg123":
				args = []string{"-q", path}
			case "mpv":
				args = []string{"--no-video", "--really-quiet", path}
			}
			return run(command, args...)
		}
		d.warnSound(fmt.Errorf("sound playback: no command found (paplay, pw-play, ffplay, mpg123, mpv)"))
		return nil
	default:
		return fmt.Errorf("unsupported: sound playback on %s", runtime.GOOS)
	}
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
