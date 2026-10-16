package config

import (
	"os"
	"strings"
)

// LoadReloaded keeps the previous UI table when a UI item cannot be decoded.
// Startup decoding still uses defaults; live reload must not discard active UI settings.
func LoadReloaded(current Config) (Config, error) {
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		data, err = nil, nil
	}
	if err != nil {
		return Config{}, err
	}
	c, err := Decode(data)
	if err != nil {
		return Config{}, err
	}
	for i, warning := range c.Warnings {
		if strings.HasPrefix(warning, "config parse error: ui.") && strings.Contains(warning, "; invalid type,") {
			c.UI = current.UI
			c.Warnings[i] = "invalid ui config: " + warning + "; keeping current ui settings"
		}
	}
	return c, nil
}

func (e Edit) ApplyTo(current Config) (Config, error) {
	if e.Kind == "reload" {
		return LoadReloaded(current)
	}
	return e.Apply()
}
