package config

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"strconv"
	"strings"
)

type PluginKeyBinding struct {
	Key         string    `toml:"key"`
	Type        string    `toml:"type"`
	Command     string    `toml:"command"`
	Description string    `toml:"description"`
	Width       Dimension `toml:"width"`
	Height      Dimension `toml:"height"`
}

func (c Config) ValidatePluginKeys() error {
	for _, binding := range c.Keys.Command {
		if binding.Type != "plugin_action" && binding.Type != "pane" && binding.Type != "popup" && binding.Type != "shell" {
			return fmt.Errorf("unsupported keys.command type %q", binding.Type)
		}
		if chord, err := ParseChord(binding.Key); err != nil || chord.Indexed {
			return fmt.Errorf("unsupported plugin action key %q: expected prefix+key", binding.Key)
		}
		if binding.Command == "" {
			return fmt.Errorf("plugin_action requires command")
		}
	}
	return nil
}

// Dimension accepts terminal cells and percentage strings.
type Dimension struct{ Size *api.PopupSize }

func (d *Dimension) UnmarshalTOML(v any) error {
	var size api.PopupSize
	switch v := v.(type) {
	case int64:
		size.Cells = int(v)
	case string:
		if strings.HasSuffix(v, "%") {
			n, err := strconv.Atoi(strings.TrimSuffix(v, "%"))
			if err != nil {
				return err
			}
			size.Percent = n
		} else {
			n, err := strconv.Atoi(v)
			if err != nil {
				return err
			}
			size.Cells = n
		}
	default:
		return fmt.Errorf("dimension requires cells or percentage")
	}
	if size.Cells <= 0 && size.Percent <= 0 || size.Percent > 100 {
		return fmt.Errorf("invalid popup dimension")
	}
	d.Size = &size
	return nil
}
