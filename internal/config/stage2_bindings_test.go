package config

import (
	"strings"
	"testing"
)

func TestStage2InvalidAndPrintableBindingsDisabled(t *testing.T) {
	for _, spec := range []string{"not-a-key", "a"} {
		c, err := Decode([]byte("[keys]\nhelp=" + `"` + spec + `"` + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		if b := c.ActionBindings("help"); len(b) != 1 || b[0] != "" {
			t.Fatal(spec, b)
		}
		if !strings.Contains(c.Warning(), "help") {
			t.Fatal("missing diagnostic", c.Warning())
		}
	}
}
func TestStage2BindingConflict(t *testing.T) {
	c, err := Decode([]byte("[keys]\nhelp='cmd+y'\nsettings='cmd+y'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if b := c.ActionBindings("help"); len(b) != 1 || b[0] != "cmd+y" {
		t.Fatal(b)
	}
	if b := c.ActionBindings("settings"); len(b) != 1 || b[0] != "" {
		t.Fatal(b)
	}
	if !strings.Contains(c.Warning(), "settings") {
		t.Fatal(c.Warning())
	}
}

func TestStage2InvalidArrayKeepsOtherBindings(t *testing.T) {
	c, err := Decode([]byte("[keys]\nhelp=['not-a-key','cmd+y']\n"))
	if err != nil {
		t.Fatal(err)
	}
	if b := c.ActionBindings("help"); len(b) != 1 || b[0] != "cmd+y" {
		t.Fatal(b)
	}
	if len(c.Warnings) != 1 {
		t.Fatal(c.Warnings)
	}
}
