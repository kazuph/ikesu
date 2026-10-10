package config

import (
	"fmt"
	"strings"
)

func (c *Config) validateValues() {
	enums := []struct {
		key     string
		value   *string
		allowed []string
	}{
		{"terminal.shell_mode", &c.Terminal.ShellMode, []string{"auto", "login", "non_login"}},
		{"update.channel", &c.Update.Channel, []string{"stable", "preview"}},
		{"ui.sidebar_collapsed_mode", &c.UI.SidebarCollapsedMode, []string{"compact", "hidden"}},
		{"ui.host_cursor", &c.UI.HostCursor, []string{"auto", "native", "drawn"}},
		{"ui.agent_panel_sort", &c.UI.AgentPanelSort, []string{"spaces", "workspaces", "priority"}},
		{"ui.agent_panel_scope", &c.UI.AgentPanelScope, []string{"all", "sort", "current"}},
		{"ui.workspace_panel_density", &c.UI.WorkspacePanelDensity, []string{"full", "slim"}},
		{"ui.toast.delivery", &c.UI.Toast.Delivery, []string{"off", "herdr", "gtx", "terminal", "system"}},
		{"ui.toast.herdr.position", &c.UI.Toast.Herdr.Position, []string{"top-left", "top-right", "bottom-left", "bottom-right"}},
		{"ui.toast.clipboard.position", &c.UI.Toast.Clipboard.Position, []string{"top-left", "top-center", "top-right", "bottom-left", "bottom-center", "bottom-right"}},
		{"experimental.cjk_ime_cursor_shape", &c.Experimental.CJKIMECursorShape, []string{"block", "steady_block", "underline", "steady_underline", "bar", "steady_bar"}},
		{"ui.right_click_passthrough_modifier", &c.UI.RightClickPassthroughModifier, []string{"off", "ctrl", "alt", "cmd", "super", "ctrl+alt", "ctrl+cmd", "alt+cmd", "ctrl+alt+cmd"}},
	}
	for _, e := range enums {
		if *e.value == "" {
			continue
		}
		ok := false
		for _, value := range e.allowed {
			ok = ok || value == *e.value
		}
		if !ok {
			c.warn(e.key, fmt.Sprintf("unknown value %q; using default", *e.value))
			*e.value = ""
		}
	}
	for _, name := range []struct {
		key   string
		value *string
	}{{"theme.dark_name", &c.Theme.DarkName}, {"theme.light_name", &c.Theme.LightName}} {
		if *name.value != "" {
			if err := ValidateTheme(*name.value); err != nil {
				c.warn(name.key, err.Error())
				*name.value = ""
			}
		}
	}
	for name, value := range c.UI.Sound.Agents {
		if value != "default" && value != "on" && value != "off" {
			c.warn("ui.sound.agents."+name, "unknown sound override; using default")
			delete(c.UI.Sound.Agents, name)
		}
	}
	for _, n := range []struct {
		key       string
		value     **int
		allowZero bool
	}{
		{"ui.sidebar_width", &c.UI.SidebarWidth, false}, {"ui.sidebar_min_width", &c.UI.SidebarMinWidth, false}, {"ui.sidebar_max_width", &c.UI.SidebarMaxWidth, false}, {"ui.mouse_scroll_lines", &c.UI.MouseScrollLines, false}, {"ui.mobile_width_threshold", &c.UI.MobileWidthThreshold, true}, {"ui.toast.delay_seconds", &c.UI.Toast.DelaySeconds, true}, {"agent_restore.restore_delay_ms", &c.AgentRestore.RestoreDelayMs, true}, {"advanced.scrollback_limit_bytes", &c.Advanced.ScrollbackLimitBytes, true},
	} {
		if *n.value != nil && (**n.value < 0 || **n.value == 0 && !n.allowZero) {
			c.warn(n.key, "out of range; using default")
			*n.value = nil
		}
	}
	for agent, command := range c.AgentRestore.Commands {
		if !strings.Contains(command, "{session_id}") {
			c.warn("agent_restore.commands."+agent, "requires {session_id}; using default")
			delete(c.AgentRestore.Commands, agent)
		}
	}
	for agent, argv := range c.AgentStart.Commands {
		if len(argv) == 0 || argv[0] == "" {
			c.warn("agent_start.commands."+agent, "requires non-empty executable")
			delete(c.AgentStart.Commands, agent)
		}
	}
	if c.UI.Accent != "" {
		if _, err := ParseThemeColor(c.UI.Accent); err != nil {
			c.warn("ui.accent", err.Error())
			c.UI.Accent = ""
		}
	}
}

// Reading an unavailable feature never enables a similar substitute.
func (c *Config) unsupportedValues() {
	warn := func(key string, on bool) {
		if on {
			c.warn(key, "unsupported: this feature is not implemented")
		}
	}
	warn("update.channel", c.Update.Channel != "")
	warn("update.version_check", c.Update.VersionCheck != nil && *c.Update.VersionCheck)
	warn("update.manifest_check", c.Update.ManifestCheck != nil && *c.Update.ManifestCheck)
	warn("experimental.pane_history", c.Experimental.PaneHistory)
	warn("experimental.switch_ascii_input_source_in_prefix", c.Experimental.SwitchASCIIInputSourceInPrefix)
	warn("experimental.reveal_hidden_cursor_for_cjk_ime", c.Experimental.RevealHiddenCursorForCJKIME)
	warn("experimental.cjk_ime_agents", len(c.Experimental.CJKIMEAgents) > 0)
	warn("experimental.cjk_ime_cursor_shape", c.Experimental.CJKIMECursorShape != "")
	warn("remote.manage_ssh_config", c.Remote.ManageSSHConfig != nil && *c.Remote.ManageSSHConfig)
	warn("advanced.scrollback_limit_bytes", c.Advanced.ScrollbackLimitBytes != nil)
	warn("ui.sound.enabled", c.UI.Sound.Enabled != nil && *c.UI.Sound.Enabled)
	warn("theme.custom.blue", c.Theme.Custom["blue"] != "")
	warn("ui.sound.path", c.UI.Sound.Path != "")
	warn("ui.sound.done_path", c.UI.Sound.DonePath != "")
	warn("ui.sound.request_path", c.UI.Sound.RequestPath != "")
	for agent := range c.UI.Sound.Agents {
		warn("ui.sound.agents."+agent, true)
	}
	for _, action := range []string{"remote_image_paste"} {
		if bindings, explicit := c.Keys.Actions[action]; explicit {
			for _, binding := range bindings {
				if binding != "" {
					warn("keys."+action, true)
					break
				}
			}
		}
	}
	warn("ui.pane_gaps", c.UI.PaneGaps != nil && !*c.UI.PaneGaps)
}
func (c Config) MouseScrollLines() int {
	if c.UI.MouseScrollLines != nil {
		return *c.UI.MouseScrollLines
	}
	return 3
}

func (c *Config) applyIndexedBindings() {
	for _, field := range []struct {
		key, action string
		value       *string
	}{
		{"tabs", "switch_tab", &c.Keys.Indexed.Tabs}, {"workspaces", "switch_workspace", &c.Keys.Indexed.Workspaces}, {"agents", "focus_agent", &c.Keys.Indexed.Agents},
	} {
		if *field.value == "" {
			continue
		}
		spec := *field.value + "+1..9"
		if _, err := ParseChord(spec); err != nil {
			c.warn("keys.indexed."+field.key, err.Error())
			*field.value = ""
			continue
		}
		if _, explicit := c.Keys.Actions[field.action]; !explicit {
			c.Keys.Actions[field.action] = Bindings{spec}
		}
	}
}
func (c *Config) validateSidebarItems() {
	for _, panel := range []struct {
		name  string
		value *SidebarPanel
	}{{"agents", &c.UI.Sidebar.Agents}, {"spaces", &c.UI.Sidebar.Spaces}} {
		path := "ui.sidebar." + panel.name
		if panel.value.RowGap < 0 {
			c.warn(path+".row_gap", "must not be negative; using default")
			panel.value.RowGap = 0
		}
		clean := func(rows [][]SidebarToken, path string) [][]SidebarToken {
			for i, row := range rows {
				valid := row[:0]
				for j, token := range row {
					one := Config{}
					p := SidebarPanel{Rows: [][]SidebarToken{{token}}}
					if panel.name == "agents" {
						one.UI.Sidebar.Agents = p
					} else {
						one.UI.Sidebar.Spaces = p
					}
					if err := one.ValidateSidebar(); err != nil {
						c.warn(fmt.Sprintf("%s[%d][%d]", path, i, j), err.Error())
					} else {
						valid = append(valid, token)
					}
				}
				rows[i] = valid
			}
			return rows
		}
		panel.value.Rows = clean(panel.value.Rows, path+".rows")
		for agent, rows := range panel.value.RowsByAgent {
			panel.value.RowsByAgent[agent] = clean(rows, path+".rows_by_agent."+agent)
		}
	}
}
