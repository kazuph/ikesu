package config

import (
	"bytes"
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
)

func (c Config) SidebarBounds() (lo, hi, normal int, err error) {
	lo, hi, normal = 18, 72, 26
	if c.UI.SidebarMinWidth != nil {
		lo = *c.UI.SidebarMinWidth
	}
	if c.UI.SidebarMaxWidth != nil {
		hi = *c.UI.SidebarMaxWidth
	}
	if c.UI.SidebarWidth != nil {
		normal = *c.UI.SidebarWidth
	}
	if lo < 1 || hi < 1 || lo > hi {
		err = fmt.Errorf("invalid sidebar bounds: %d..%d; using 18..72", lo, hi)
		lo, hi = 18, 72
	}
	normal = max(lo, min(hi, normal))
	return
}
func (c Config) WorkspaceDensity() string {
	if c.UI.WorkspacePanelDensity == "slim" {
		return "slim"
	}
	return "full"
}
func (c Config) AgentScope() string {
	if c.UI.AgentPanelScope == "sort" {
		return "sort"
	}
	return "all"
}

// SaveSidebarSetting preserves every other TOML value and atomically replaces the file.
func SaveSidebarSetting(key, value string) error {
	p := Path()
	m := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		if _, err = toml.Decode(string(b), &m); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	ui, ok := m["ui"].(map[string]any)
	if !ok {
		ui = map[string]any{}
		m["ui"] = ui
	}
	ui[key] = value
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".sidebar-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b.Bytes())
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	return os.Rename(f.Name(), p)
}
