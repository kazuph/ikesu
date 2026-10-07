// Package config loads ~/.config/gtx/config.toml (override with
// $GTX_CONFIG). The AI-link stage reads [ui.toast] delivery,
// [experimental] allow_nested, [keys] open_notification_target, and
// [agent] shell/env.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// EnvVar overrides the config file location.
const EnvVar = "GTX_CONFIG"

// ToastDelivery selects where agent notifications go.
type ToastDelivery string

const (
	ToastOff      ToastDelivery = "off"      // default: no notifications
	ToastGTX      ToastDelivery = "gtx"      // in-app toast
	ToastTerminal ToastDelivery = "terminal" // outer terminal (OSC 9)
	ToastSystem   ToastDelivery = "system"   // OS notification
)

// Valid reports whether d is a known delivery value.
func (d ToastDelivery) Valid() bool {
	switch d {
	case ToastOff, ToastGTX, ToastTerminal, ToastSystem:
		return true
	}
	return false
}

// Config is the parsed config file.
type Config struct {
	UI struct {
		Toast struct {
			Delivery string `toml:"delivery"`
			// Enabled is the legacy boolean: true→"gtx", false→"off";
			// delivery wins when both are present.
			Enabled *bool `toml:"enabled"`
		} `toml:"toast"`
	} `toml:"ui"`
	Experimental struct {
		AllowNested bool `toml:"allow_nested"`
	} `toml:"experimental"`
	Keys struct {
		OpenNotificationTarget string `toml:"open_notification_target"`
	} `toml:"keys"`
	Agent struct {
		Shell string            `toml:"shell"`
		Env   map[string]string `toml:"env"`
	} `toml:"agent"`
	// AgentStart holds structured argv templates used to launch agents;
	// agent restore reuses the same argv so sandbox/approval/wrapper
	// flags stay identical between first start and resume.
	AgentStart struct {
		Commands map[string][]string `toml:"commands"`
	} `toml:"agent_start"`
	Session struct {
		// ResumeAgentsOnRestore resumes recorded agent sessions after a
		// session.json restore. Default on.
		ResumeAgentsOnRestore *bool `toml:"resume_agents_on_restore"`
	} `toml:"session"`
	AgentRestore struct {
		// RestoreDelayMs postpones automatic resume after server start.
		RestoreDelayMs *int `toml:"restore_delay_ms"`
		// Templates are per-agent restore command lines; each must contain
		// {session_id}. Missing entries fall back to builtins.
		Templates map[string]string `toml:"templates"`
	} `toml:"agent_restore"`
}

// Path is the resolved config file location.
func Path() string {
	if p := os.Getenv(EnvVar); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gtx", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("gtx", "config.toml")
	}
	return filepath.Join(home, ".config", "gtx", "config.toml")
}

// Load reads the config file; a missing file is not an error.
func Load() (Config, error) {
	var c Config
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if _, err := toml.Decode(string(data), &c); err != nil {
		return c, err
	}
	return c, nil
}

// ToastDelivery resolves the effective delivery mode. Invalid values fall
// back to off (notifications must fail closed, not surprise the user).
func (c Config) ToastDelivery() ToastDelivery {
	switch {
	case c.UI.Toast.Delivery != "":
		if d := ToastDelivery(c.UI.Toast.Delivery); d.Valid() {
			return d
		}
		return ToastOff
	case c.UI.Toast.Enabled != nil:
		if *c.UI.Toast.Enabled {
			return ToastGTX
		}
		return ToastOff
	}
	return ToastOff
}

// AllowNested reports whether nested gtx sessions are permitted.
func (c Config) AllowNested() bool { return c.Experimental.AllowNested }

// NavKey returns the open-notification-target key spec, default
// "prefix+o". Only "prefix+<key>" forms are supported in this stage.
func (c Config) NavKey() string {
	if k := strings.TrimSpace(c.Keys.OpenNotificationTarget); k != "" {
		return k
	}
	return "prefix+o"
}

// ParseNavKey maps a key spec like "prefix+o" or "prefix+G" to the plain
// key rune that follows the prefix; ok=false for unsupported specs.
func ParseNavKey(spec string) (rune, bool) {
	rest, ok := strings.CutPrefix(spec, "prefix+")
	if !ok {
		return 0, false
	}
	runes := []rune(rest)
	if len(runes) != 1 {
		return 0, false
	}
	return runes[0], true
}

// AgentEnv returns the env map merged into agent.start panes.
func (c Config) AgentEnv() map[string]string { return c.Agent.Env }

// AgentShell is the shell argv used by `agent start --shell`; it defaults
// to /bin/zsh like herdr.
func (c Config) AgentShell() string {
	if s := strings.TrimSpace(c.Agent.Shell); s != "" {
		return s
	}
	return "/bin/zsh"
}

// AgentStartCommand returns the configured argv for an agent, if any.
func (c Config) AgentStartCommand(agent string) ([]string, bool) {
	argv, ok := c.AgentStart.Commands[agent]
	if !ok || len(argv) == 0 {
		return nil, false
	}
	return append([]string{}, argv...), true
}

// RestoreTemplate returns the configured restore command template for an
// agent, if any. The template must contain {session_id} to be usable.
func (c Config) RestoreTemplate(agent string) string {
	return c.AgentRestore.Templates[agent]
}

// ResumeAgentsOnRestore reports whether agent panes resume automatically
// after a session.json restore; it defaults to on.
func (c Config) ResumeAgentsOnRestore() bool {
	if c.Session.ResumeAgentsOnRestore == nil {
		return true
	}
	return *c.Session.ResumeAgentsOnRestore
}

// RestoreDelay is the configured post-startup auto-resume delay.
func (c Config) RestoreDelay() int {
	if c.AgentRestore.RestoreDelayMs == nil {
		return 0
	}
	if *c.AgentRestore.RestoreDelayMs < 0 {
		return 0
	}
	return *c.AgentRestore.RestoreDelayMs
}
