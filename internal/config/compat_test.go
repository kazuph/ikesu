package config

import (
	uv "github.com/charmbracelet/ultraviolet"
	"strings"
	"testing"
)

func TestCompatibilityStringsArraysAndPartialFailure(t *testing.T) {
	c, err := Decode([]byte(`onboarding=false
[theme]
name="tokyo-night"
[ui]
sidebar_width="wrong"
show_tab_bar=false
[keys]
split_vertical="prefix+v"
focus_history_back="cmd+["
rename_pane=["prefix+shift+p"]
next_workspace="cmd+j"
[[keys.command]]
key="prefix+t"
type="popup"
command="exec /bin/sh"
width="80%"
height=12
[agent_restore]
enabled=false
[agent_restore.commands]
codex="codex resume {session_id}"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warning(), "ui.sidebar_width") {
		t.Fatal(c.Warnings)
	}
	if c.ThemeName() != "tokyo-night" || c.ShowTabBar() || c.LegacyRestoreEnabled() || c.Onboarding == nil || *c.Onboarding {
		t.Fatal("valid values lost", c)
	}
	if c.Keys.Command[0].Width.Size.Percent != 80 || c.Keys.Command[0].Height.Size.Cells != 12 {
		t.Fatal(c.Keys.Command)
	}
	if c.Keys.FocusHistoryBack[0] != "cmd+[" || c.Keys.RenamePane[0] != "prefix+shift+p" || c.RestoreTemplate("codex") != "codex resume {session_id}" {
		t.Fatal(c)
	}
}
func TestInvalidValueResetsOnlyOwnItem(t *testing.T) {
	c, err := Decode([]byte(`[theme]
name="invalid"
[theme.custom]
accent="invalid"
surface0="#123456"
[keys]
next_workspace=42
previous_workspace="cmd+k"
[[keys.command]]
key="prefix+x"
type="bad"
command="true"
[[keys.command]]
key="prefix+t"
type="pane"
command="true"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Warnings) != 4 || c.ThemeName() != "catppuccin" || c.Theme.Custom["surface0"] != "#123456" || len(c.Keys.Command) != 1 || c.Keys.Actions["previous_workspace"][0] != "cmd+k" {
		t.Fatal(c.Warnings, c)
	}
}
func TestChordKittySuperAndPrefix(t *testing.T) {
	// CSI 106;9u decodes to j + Super through the existing input decoder.
	for _, spec := range []string{"cmd+j", "super+[", "prefix+shift+r", "ctrl+t", "cmd+1..9", "f12", "prefix+minus"} {
		if _, err := ParseChord(spec); err != nil {
			t.Fatal(spec, err)
		}
	}
}

func TestCompatibilityEnumsAndSidebarItemIsolation(t *testing.T) {
	c, err := Decode([]byte(`[ui]
agent_panel_scope="unknown"
mouse_scroll_lines=7
[ui.sidebar.agents]
row_gap=-1
rows=[["agent","bogus","workspace"]]
[ui.sidebar.spaces]
rows=[["branch"]]
[keys.indexed]
workspaces="cmd+shift"
`))
	if err != nil || len(c.Warnings) != 3 {
		t.Fatal(err, c.Warnings)
	}
	if c.MouseScrollLines() != 7 || len(c.UI.Sidebar.Agents.Rows[0]) != 2 || c.UI.Sidebar.Spaces.Rows[0][0].Token != "branch" || c.ActionBindings("switch_workspace")[0] != "cmd+shift+1..9" {
		t.Fatal(c)
	}
}
func TestUnsupportedValuesPreserveSupportedNeighbours(t *testing.T) {
	c, err := Decode([]byte(`onboarding=false
[experimental]
kitty_graphics=true
[ui]
show_tab_bar=false
[advanced]
scrollback_limit_bytes=65536
`))
	if err != nil || len(c.Warnings) != 2 || c.ShowTabBar() || *c.Onboarding {
		t.Fatal(c, err)
	}
	for _, warning := range c.Warnings {
		if !strings.Contains(warning, "unsupported:") {
			t.Fatal(warning)
		}
	}
}

func TestInvalidTypesNeverDiscardValidTheme(t *testing.T) {
	for _, field := range []string{"mouse_capture", "copy_on_select", "mouse_scroll_lines", "confirm_close", "prompt_new_tab_name", "pane_borders", "sidebar_width", "show_tab_bar"} {
		t.Run(field, func(t *testing.T) {
			c, err := Decode([]byte("[theme]\nname=\"tokyo-night\"\n[ui]\n" + field + "={bad=1}\n"))
			if err != nil || len(c.Warnings) != 1 || c.ThemeName() != "tokyo-night" {
				t.Fatal(err, c.Warnings, c.ThemeName())
			}
		})
	}
}

func TestLegacyUppercasePrefixBindingsRemainShifted(t *testing.T) {
	c, err := Decode([]byte(`[keys]
open_notification_target="prefix+G"
split_vertical=["prefix+V"]
`))
	if err != nil || len(c.Warnings) > 0 {
		t.Fatal(err, c.Warnings)
	}
	for _, spec := range []string{c.ActionBindings("open_notification_target")[0], c.ActionBindings("split_vertical")[0]} {
		chord, err := ParseChord(spec)
		if err != nil || !chord.Match(uv.Key{Code: rune(strings.ToUpper(spec[len(spec)-1:])[0])}, true) {
			t.Fatal(spec, chord, err)
		}
	}
}
