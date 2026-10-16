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

// The real 0.2.57 accepts both punctuation forms without a conflict warning,
// even though the help action wins for the observed input in either order.
func TestStage2PunctuationBindingsCoexist(t *testing.T) {
	for _, prefix := range []string{"prefix+", "cmd+"} {
		for _, reverse := range []bool{false, true} {
			help := "help='" + prefix + "?'\n"
			settings := "settings='" + prefix + "shift+?'\n"
			body := help + settings
			if reverse {
				body = settings + help
			}
			c, err := Decode([]byte("[keys]\n" + body))
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range map[string]string{"help": prefix + "?", "settings": prefix + "shift+?"} {
				if b := c.ActionBindings(name); len(b) != 1 || b[0] != want {
					t.Fatalf("%s: %v", name, b)
				}
			}
			if c.Warning() != "" {
				t.Fatal(c.Warning())
			}
		}
	}
}
