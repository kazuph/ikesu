package config

import (
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
