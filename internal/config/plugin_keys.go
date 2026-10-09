package config

import "fmt"

type PluginKeyBinding struct {
	Key         string `toml:"key"`
	Type        string `toml:"type"`
	Command     string `toml:"command"`
	Description string `toml:"description"`
}

func (c Config) ValidatePluginKeys() error {
	for _, binding := range c.Keys.Command {
		if binding.Type != "plugin_action" {
			return fmt.Errorf("unsupported keys.command type %q", binding.Type)
		}
		if _, ok := ParseKeyBinding(binding.Key); !ok {
			return fmt.Errorf("unsupported plugin action key %q: expected prefix+key", binding.Key)
		}
		if binding.Command == "" {
			return fmt.Errorf("plugin_action requires command")
		}
	}
	return nil
}
