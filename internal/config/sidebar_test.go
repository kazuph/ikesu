package config

import (
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"testing"
)

func TestSidebarDefaultsBoundsAndDensity(t *testing.T) {
	var c Config
	lo, hi, n, e := c.SidebarBounds()
	if lo != 18 || hi != 72 || n != 26 || e != nil || c.WorkspaceDensity() != "full" || c.AgentScope() != "all" {
		t.Fatal(lo, hi, n, e)
	}
	a, b := 80, 10
	c.UI.SidebarMinWidth = &a
	c.UI.SidebarMaxWidth = &b
	lo, hi, _, e = c.SidebarBounds()
	if e == nil || lo != 18 || hi != 72 {
		t.Fatal("invalid bounds")
	}
	c.UI.AgentPanelScope = "current"
	if c.AgentScope() != "all" {
		t.Fatal("legacy current")
	}
}
func TestSidebarSavePreservesConfigAndFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(EnvVar, p)
	if err := os.WriteFile(p, []byte("[agent]\nshell = '/bin/sh'\n[ui]\nsidebar_max_width = 72\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveSidebarSetting("workspace_panel_density", "slim"); err != nil {
		t.Fatal(err)
	}
	c, e := Load()
	if e != nil || c.AgentShell() != "/bin/sh" || c.WorkspaceDensity() != "slim" || *c.UI.SidebarMaxWidth != 72 {
		t.Fatal(c, e)
	}
	if err := os.WriteFile(p, []byte("invalid ["), 0600); err != nil {
		t.Fatal(err)
	}
	if SaveSidebarSetting("agent_panel_scope", "sort") == nil {
		t.Fatal("corrupt config overwritten")
	}
}

func TestG2GeneratedDefaultConfig(t *testing.T) {
	var c Config
	if _, err := toml.Decode(DefaultExample, &c); err != nil {
		t.Fatal(err)
	}
	lo, hi, n, err := c.SidebarBounds()
	if err != nil || lo != 18 || hi != 72 || n != 26 || c.WorkspaceDensity() != "full" || c.AgentScope() != "all" {
		t.Fatal(c)
	}
}
