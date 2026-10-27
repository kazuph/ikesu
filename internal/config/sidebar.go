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
	if c.UI.AgentPanelScope == "sort" || c.UI.AgentPanelSort == "priority" {
		return "sort"
	}
	return "all"
}

func SaveAgentSort(value string) error {
	if value != "spaces" && value != "priority" {
		return fmt.Errorf("unsupported agent panel sort: %s", value)
	}
	return saveValues([]string{"ui"}, map[string]any{"agent_panel_scope": "all", "agent_panel_sort": value})
}

// SaveSidebarSetting preserves every other TOML value and atomically replaces the file.
func SaveSidebarSetting(key, value string) error {
	return saveValues([]string{"ui"}, map[string]any{key: value})
}

func saveValues(section []string, values map[string]any) error {
	p := Path()
	m := map[string]any{}
	if b, err := os.ReadFile(p); err == nil {
		if _, err = toml.Decode(string(b), &m); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	target := m
	for _, name := range section {
		table, ok := target[name].(map[string]any)
		if !ok {
			table = map[string]any{}
			target[name] = table
		}
		target = table
	}
	for key, value := range values {
		target[key] = value
	}
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

// The distribution documents mobile layout at or below 64 cells.
func (c Config) MobileWidthThreshold() int {
	if c.UI.MobileWidthThreshold != nil {
		return max(0, *c.UI.MobileWidthThreshold)
	}
	return 64
}

func (c Config) ThemeName() string {
	if c.Theme.ResolvedName != "" {
		return c.Theme.ResolvedName
	}
	if c.Theme.Name == "" {
		return "catppuccin"
	}
	return c.Theme.Name
}
func (c Config) SoundEnabled() bool { return c.UI.Sound.Enabled == nil || *c.UI.Sound.Enabled }

func SaveTheme(name string) error {
	if err := ValidateTheme(name); err != nil {
		return err
	}
	return saveValues([]string{"theme"}, map[string]any{"name": name, "auto_switch": false})
}
func SaveSound(enabled bool) error {
	return saveValues([]string{"ui", "sound"}, map[string]any{"enabled": enabled})
}
func SaveToast(delivery ToastDelivery) error {
	if !delivery.Valid() {
		return fmt.Errorf("unsupported toast delivery: %s", delivery)
	}
	return saveValues([]string{"ui", "toast"}, map[string]any{"delivery": string(delivery)})
}

func SaveOnboarding(enabled bool) error {
	return saveValues(nil, map[string]any{"onboarding": enabled})
}

// KoiEnabled follows the existing optional-boolean convention: unset means on.
func (c Config) KoiEnabled() bool { return c.UI.Koi == nil || *c.UI.Koi }
