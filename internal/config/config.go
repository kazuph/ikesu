// Package config loads ~/.config/gtx/config.toml (override with
// $GTX_CONFIG). The AI-link stage reads [ui.toast] delivery,
// [experimental] allow_nested, [keys] open_notification_target, and
// [agent] shell/env.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/guard"
)

// EnvVar overrides the config file location.
const EnvVar = "GTX_CONFIG"

// Distribution default-config: per-pane scrollback byte budget.
const DefaultScrollbackLimitBytes = 10000000

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
	Decisions struct {
		ScreenPrompts *bool `toml:"screen_prompts"`
	} `toml:"decisions"`
	Guard      guard.Config   `toml:"guard"`
	Warnings   []string       `toml:"-"`
	Onboarding *bool          `toml:"onboarding"`
	Terminal   TerminalConfig `toml:"terminal"`
	Update     UpdateConfig   `toml:"update"`
	Remote     struct {
		ManageSSHConfig *bool `toml:"manage_ssh_config"`
	} `toml:"remote"`
	Advanced struct {
		ScrollbackLimitBytes *int `toml:"scrollback_limit_bytes"`
	} `toml:"advanced"`
	Worktrees struct {
		Directory string `toml:"directory"`
	} `toml:"worktrees"`
	Theme struct {
		ResolvedName string            `toml:"-"`
		AutoSwitch   bool              `toml:"auto_switch"`
		DarkName     string            `toml:"dark_name"`
		LightName    string            `toml:"light_name"`
		Name         string            `toml:"name"`
		Custom       map[string]string `toml:"custom"`
	} `toml:"theme"`
	UI struct {
		MouseCapture                  *bool         `toml:"mouse_capture"`
		CopyOnSelect                  *bool         `toml:"copy_on_select"`
		HostCursor                    string        `toml:"host_cursor"`
		RightClickPassthroughModifier string        `toml:"right_click_passthrough_modifier"`
		RedrawOnFocusGained           *bool         `toml:"redraw_on_focus_gained"`
		MouseScrollLines              *int          `toml:"mouse_scroll_lines"`
		ConfirmClose                  *bool         `toml:"confirm_close"`
		PromptNewTabName              *bool         `toml:"prompt_new_tab_name"`
		PaneGaps                      *bool         `toml:"pane_gaps"`
		SidebarCollapsedMode          string        `toml:"sidebar_collapsed_mode"`
		Accent                        string        `toml:"accent"`
		MobileWidthThreshold          *int          `toml:"mobile_width_threshold"`
		Sidebar                       SidebarPanels `toml:"sidebar"`
		RightSidebarWidth             *int          `toml:"right_sidebar_width"`
		SidebarWidth                  *int          `toml:"sidebar_width"`
		SidebarMinWidth               *int          `toml:"sidebar_min_width"`
		SidebarMaxWidth               *int          `toml:"sidebar_max_width"`
		SidebarStartCollapsed         bool          `toml:"sidebar_start_collapsed"`
		TransparentSidebarCards       bool          `toml:"transparent_sidebar_cards"`
		WorkspacePanelDensity         string        `toml:"workspace_panel_density"`
		AgentPanelScope               string        `toml:"agent_panel_scope"`
		AgentPanelSort                string        `toml:"agent_panel_sort"`
		Sound                         struct {
			Enabled     *bool             `toml:"enabled"`
			Path        string            `toml:"path"`
			DonePath    string            `toml:"done_path"`
			RequestPath string            `toml:"request_path"`
			Agents      map[string]string `toml:"agents"`
		} `toml:"sound"`
		Toast struct {
			Delivery     string `toml:"delivery"`
			DelaySeconds *int   `toml:"delay_seconds"`
			Herdr        struct {
				Position string `toml:"position"`
			} `toml:"herdr"`
			Clipboard struct {
				Enabled  *bool  `toml:"enabled"`
				Position string `toml:"position"`
			} `toml:"clipboard"`
			// Enabled is the legacy boolean: true→"gtx", false→"off";
			// delivery wins when both are present.
			Enabled *bool `toml:"enabled"`
		} `toml:"toast"`
		// ShowTabBar (default true) hides the desktop tab bar entirely
		// when false; HideTabBarWhenSingleTab only additionally hides a
		// lone tab. PaneBorders (default true) draws pane frames.
		ShowAgentLabelsOnPaneBorders bool  `toml:"show_agent_labels_on_pane_borders"`
		ShowTabBar                   *bool `toml:"show_tab_bar"`
		HideTabBarWhenSingleTab      *bool `toml:"hide_tab_bar_when_single_tab"`
		PaneBorders                  *bool `toml:"pane_borders"`
	} `toml:"ui"`
	Experimental struct {
		AllowNested                    bool     `toml:"allow_nested"`
		KittyGraphics                  bool     `toml:"kitty_graphics"`
		PaneHistory                    bool     `toml:"pane_history"`
		SwitchASCIIInputSourceInPrefix bool     `toml:"switch_ascii_input_source_in_prefix"`
		RevealHiddenCursorForCJKIME    bool     `toml:"reveal_hidden_cursor_for_cjk_ime"`
		CJKIMEAgents                   []string `toml:"cjk_ime_agents"`
		CJKIMECursorShape              string   `toml:"cjk_ime_cursor_shape"`
	} `toml:"experimental"`
	Keys struct {
		Prefix  string              `toml:"prefix"`
		Actions map[string]Bindings `toml:"-"`
		Indexed struct {
			Tabs       string `toml:"tabs"`
			Workspaces string `toml:"workspaces"`
			Agents     string `toml:"agents"`
		} `toml:"indexed"`
		Command                []PluginKeyBinding `toml:"command"`
		OpenNotificationTarget string             `toml:"open_notification_target"`
		CopyMode               Bindings           `toml:"copy_mode"`
		SplitVertical          Bindings           `toml:"split_vertical"`
		SplitHorizontal        Bindings           `toml:"split_horizontal"`
		RenamePane             Bindings           `toml:"rename_pane"`
		EditScrollback         Bindings           `toml:"edit_scrollback"`
		FocusHistoryBack       Bindings           `toml:"focus_history_back"`
		FocusHistoryForward    Bindings           `toml:"focus_history_forward"`
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
		Enabled  *bool             `toml:"enabled"`
		Commands map[string]string `toml:"commands"`
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
		return filepath.Join(dir, api.BinaryNamespace(), "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(api.BinaryNamespace(), "config.toml")
	}
	return filepath.Join(home, ".config", api.BinaryNamespace(), "config.toml")
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
	return Decode(data)
}

// ToastDelivery resolves the effective delivery mode. Invalid values fall
// back to off (notifications must fail closed, not surprise the user).
func (c Config) ToastDelivery() ToastDelivery {
	switch {
	case c.UI.Toast.Delivery != "":
		if c.UI.Toast.Delivery == "herdr" {
			return ToastGTX
		}
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

// ShowTabBar reports whether the desktop tab bar is drawn (default true).
func (c Config) ShowTabBar() bool {
	return c.UI.ShowTabBar == nil || *c.UI.ShowTabBar
}

// HideTabBarWhenSingleTab additionally hides the bar for a lone tab,
// only when ShowTabBar is on (default false).
func (c Config) HideTabBarWhenSingleTab() bool {
	return c.UI.HideTabBarWhenSingleTab != nil && *c.UI.HideTabBarWhenSingleTab
}

// PaneBorders reports whether pane frames are drawn (default true).
func (c Config) PaneBorders() bool {
	return c.UI.PaneBorders == nil || *c.UI.PaneBorders
}

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

// namedKeys maps the spec's punctuation binding names to their runes
// (SPEC G6: percent → '%', double_quote → '"').
var namedKeys = map[string]rune{
	"percent":      '%',
	"double_quote": '"',
	"minus":        '-',
	"space":        ' ',
}

// ParseKeyBinding resolves a "prefix+<key>" spec to its rune; punctuation
// names (percent, double_quote, minus) resolve to their characters.
func ParseKeyBinding(spec string) (rune, bool) {
	rest, ok := strings.CutPrefix(spec, "prefix+")
	if !ok {
		return 0, false
	}
	if r, ok := namedKeys[rest]; ok {
		return r, true
	}
	if key, shifted := strings.CutPrefix(rest, "shift+"); shifted {
		runes := []rune(strings.ToUpper(key))
		if len(runes) == 1 {
			return runes[0], true
		}
		return 0, false
	}
	runes := []rune(rest)
	if len(runes) != 1 {
		return 0, false
	}
	return runes[0], true
}

// keyList resolves a binding list; an empty or all-invalid list falls back
// to the given default specs.
func (c Config) keyList(specs []string, def []string) map[rune]bool {
	if specs == nil {
		specs = def
	}
	out := map[rune]bool{}
	for _, s := range specs {
		if r, ok := ParseKeyBinding(s); ok {
			out[r] = true
		}
	}
	return out
}

// CopyKeys is the copy-mode entry set (default prefix+[).
func (c Config) CopyKeys() map[rune]bool {
	return c.keyList(c.Keys.CopyMode, []string{"prefix+["})
}

// SplitVerticalKeys is the side-by-side split set (default prefix+v).
func (c Config) SplitVerticalKeys() map[rune]bool {
	return c.keyList(c.Keys.SplitVertical, []string{"prefix+v"})
}

// SplitHorizontalKeys is the stacked split set (default prefix+-).
func (c Config) SplitHorizontalKeys() map[rune]bool {
	return c.keyList(c.Keys.SplitHorizontal, []string{"prefix+minus"})
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
	if value, ok := c.AgentRestore.Commands[agent]; ok {
		return value
	}
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
		if c.LegacyRestoreEnabled() {
			return 3000
		}
		return 0
	}
	if *c.AgentRestore.RestoreDelayMs < 0 {
		return 0
	}
	return *c.AgentRestore.RestoreDelayMs
}
