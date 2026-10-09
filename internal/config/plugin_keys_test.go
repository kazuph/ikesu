package config

import "testing"

func TestPluginActionKeyValidation(t *testing.T) {
	var cfg Config
	cfg.Keys.Command = []PluginKeyBinding{{Key: "prefix+l", Type: "plugin_action", Command: "example.apply"}}
	if err := cfg.ValidatePluginKeys(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []PluginKeyBinding{{Key: "prefix+l", Type: "plugin_action"}, {Key: "invalid", Type: "plugin_action", Command: "example.apply"}, {Key: "prefix+l", Type: "unknown", Command: "example.apply"}} {
		cfg.Keys.Command = []PluginKeyBinding{bad}
		if cfg.ValidatePluginKeys() == nil {
			t.Fatal(bad)
		}
	}
}
