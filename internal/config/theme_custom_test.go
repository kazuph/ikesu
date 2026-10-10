package config

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kazuph/goterrace/internal/teststate"
)

func TestCustomThemeColorForms(t *testing.T) {
	for value, want := range map[string]ansi.BasicColor{"gray": ansi.White, "white": ansi.BrightWhite, "darkgray": ansi.BrightBlack} {
		got, err := ParseThemeColor(value)
		if err != nil || got != want {
			t.Fatalf("%s: got %v, want observed ANSI %d (%v)", value, got, want, err)
		}
	}
	for _, value := range []string{"reset", "default", "none", "transparent", " TRANSPARENT "} {
		c, err := ParseThemeColor(value)
		if err != nil || c != nil {
			t.Fatalf("%q: %v %v", value, c, err)
		}
	}
	for _, value := range []string{"#123456", "rgb(18, 52, 86)"} {
		c, err := ParseThemeColor(value)
		if err != nil || c != (color.RGBA{18, 52, 86, 255}) {
			t.Fatalf("%q: %v %v", value, c, err)
		}
	}
	for _, value := range []string{"", "#12345", "#gggggg", "rgb(-1,0,0)", "rgb(256,0,0)", "rgb(1,2)", "unknown"} {
		if _, err := ParseThemeColor(value); err == nil {
			t.Fatalf("invalid color accepted: %q", value)
		}
	}
}

func TestCustomThemeLoadAndSavePreserveOverrides(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, EnvVar, path)
	if err := os.WriteFile(path, []byte("[theme.custom]\npanel_bg='transparent'\naccent='rgb(18,52,86)'\n[ui]\ntransparent_sidebar_cards=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme("tokyo-night"); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil || !c.UI.TransparentSidebarCards || c.Theme.Custom["panel_bg"] != "transparent" || c.Theme.Custom["accent"] != "rgb(18,52,86)" {
		t.Fatal(c, err)
	}
	c.Theme.Custom["unknown"] = "transparent"
	if c.ValidateCustomColors() == nil {
		t.Fatal("unknown override accepted")
	}
}
