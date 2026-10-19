package config

import "testing"

func TestWindowTitleConfigValues(t *testing.T) {
	for _, mode := range []string{"summary", "plain"} {
		c, err := Decode([]byte("[ui]\nwindow_title='" + mode + "'\n"))
		if err != nil || len(c.Warnings) != 0 || c.UI.WindowTitle != mode {
			t.Fatal(mode, c.Warnings, err)
		}
	}
	c, err := Decode([]byte("[ui]\nwindow_title='other'\n"))
	if err != nil || len(c.Warnings) != 1 || c.UI.WindowTitle != "" {
		t.Fatal(c.Warnings, err)
	}
	c, err = Decode([]byte(DefaultExample))
	if err != nil || c.UI.WindowTitle != "summary" {
		t.Fatal(c.Warnings, err)
	}
}
