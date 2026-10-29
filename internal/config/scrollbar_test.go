package config

import (
	"testing"
	"time"
)

func TestPaneScrollbarModesAndLegacyBooleans(t *testing.T) {
	for _, tc := range []struct {
		value, want string
		warnings    int
	}{
		{"", "auto", 0}, {`"auto"`, "auto", 0}, {`"always"`, "always", 0}, {`"off"`, "off", 0},
		{"true", "always", 0}, {"false", "off", 0}, {`"unknown"`, "auto", 1}, {"42", "auto", 1},
	} {
		text := "[ui]\n"
		if tc.value != "" {
			text += "pane_scrollbars=" + tc.value + "\n"
		}
		c, err := Decode([]byte(text))
		if err != nil || c.PaneScrollbarMode() != ScrollbarMode(tc.want) || len(c.Warnings) != tc.warnings {
			t.Fatalf("%s: %v %v %v", tc.value, c.PaneScrollbarMode(), c.Warnings, err)
		}
	}
	for _, tc := range []struct {
		value    string
		want     time.Duration
		warnings int
	}{
		{"1500", 1500 * time.Millisecond, 0}, {"2500", 2500 * time.Millisecond, 0}, {"0", 1500 * time.Millisecond, 1}, {"-1", 1500 * time.Millisecond, 1},
	} {
		c, err := Decode([]byte("[ui]\npane_scrollbar_hide_delay_ms=" + tc.value))
		if err != nil || c.PaneScrollbarHideDelay() != tc.want || len(c.Warnings) != tc.warnings {
			t.Fatal(tc, c, err)
		}
	}
}
