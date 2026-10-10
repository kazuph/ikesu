package config

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type TerminalConfig struct {
	DefaultShell string `toml:"default_shell"`
	ShellMode    string `toml:"shell_mode"`
	NewCwd       string `toml:"new_cwd"`
}
type UpdateConfig struct {
	Channel       string `toml:"channel"`
	VersionCheck  *bool  `toml:"version_check"`
	ManifestCheck *bool  `toml:"manifest_check"`
}

// Bindings preserves the array format written by Settings and also accepts
// the distribution's single-chord form. An empty string disables the action.
type Bindings []string

func (b *Bindings) UnmarshalTOML(v any) error {
	switch v := v.(type) {
	case string:
		*b = Bindings{v}
	case []any:
		values := make(Bindings, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("key binding requires strings")
			}
			values = append(values, s)
		}
		*b = values
	default:
		return fmt.Errorf("key binding requires string or array of strings")
	}
	return nil
}

// Decode rejects malformed TOML, but a wrong value only resets its own item.
func Decode(data []byte) (Config, error) {
	var c Config
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return c, err
	}
	c.Keys.Actions = map[string]Bindings{}
	if keys, ok := raw["keys"].(map[string]any); ok {
		for name := range ActionDefaults {
			if value, exists := keys[name]; exists {
				var bindings Bindings
				if err := bindings.UnmarshalTOML(value); err != nil {
					c.warn("keys."+name, err.Error())
				} else {
					valid := true
					for _, spec := range bindings {
						if spec != "" {
							if _, err := ParseChord(spec); err != nil {
								c.warn("keys."+name, err.Error())
								valid = false
								break
							}
						}
					}
					if valid {
						c.Keys.Actions[name] = bindings
					}
				}
				// Typed legacy fields remain available to existing callers.
				if !legacyBindingField(name) {
					delete(keys, name)
				} else if b, ok := c.Keys.Actions[name]; ok {
					if name == "open_notification_target" {
						if len(b) == 1 {
							keys[name] = b[0]
						} else {
							delete(keys, name)
						}
					} else {
						keys[name] = []string(b)
					}
				} else {
					delete(keys, name)
				}
			}
		}
	}
	decodeTable(raw, reflect.ValueOf(&c).Elem(), "", &c.Warnings)
	c.validateCompat()
	sort.Strings(c.Warnings)
	return c, nil
}
func legacyBindingField(s string) bool {
	switch s {
	case "copy_mode", "split_vertical", "split_horizontal", "rename_pane", "edit_scrollback", "focus_history_back", "focus_history_forward", "open_notification_target":
		return true
	}
	return false
}
func (c *Config) warn(key, message string) { c.Warnings = append(c.Warnings, key+": "+message) }
func (c Config) Warning() string           { return strings.Join(c.Warnings, "; ") }

func decodeTable(raw map[string]any, dst reflect.Value, path string, warnings *[]string) {
	fields := map[string]int{}
	for i := 0; i < dst.NumField(); i++ {
		name := dst.Type().Field(i).Tag.Get("toml")
		if name != "" && name != "-" {
			fields[name] = i
		}
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := name
		if path != "" {
			key = path + "." + name
		}
		i, ok := fields[name]
		if !ok {
			*warnings = append(*warnings, key+": unknown setting")
			continue
		}
		field := dst.Field(i)
		if field.Kind() == reflect.Struct && !field.Addr().Type().Implements(reflect.TypeOf((*interface{ UnmarshalTOML(any) error })(nil)).Elem()) {
			table, ok := raw[name].(map[string]any)
			if !ok {
				*warnings = append(*warnings, key+": expected table")
				continue
			}
			decodeTable(table, field, key, warnings)
			continue
		}
		// Decode each field into a fresh value; a failed decoder cannot leave
		// a partially populated field in the effective configuration.
		var encoded bytes.Buffer
		if err := toml.NewEncoder(&encoded).Encode(map[string]any{name: raw[name]}); err != nil {
			*warnings = append(*warnings, key+": "+err.Error())
			continue
		}
		box := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "Value", Type: field.Type(), Tag: reflect.StructTag(`toml:"` + name + `"`)}}))
		if _, err := toml.Decode(encoded.String(), box.Interface()); err != nil {
			*warnings = append(*warnings, key+": "+err.Error())
			continue
		}
		field.Set(box.Elem().Field(0))
	}
}

func (c *Config) validateCompat() {
	if err := ValidateTheme(c.ThemeName()); err != nil {
		c.warn("theme.name", err.Error())
		c.Theme.Name = ""
	}
	for key, value := range c.Theme.Custom {
		one := Config{}
		one.Theme.Custom = map[string]string{key: value}
		if err := one.ValidateCustomColors(); err != nil {
			c.warn("theme.custom."+key, err.Error())
			delete(c.Theme.Custom, key)
		}
	}
	valid := c.Keys.Command[:0]
	for i, b := range c.Keys.Command {
		one := Config{}
		one.Keys.Command = []PluginKeyBinding{b}
		if err := one.ValidatePluginKeys(); err != nil {
			c.warn(fmt.Sprintf("keys.command[%d]", i), err.Error())
		} else {
			valid = append(valid, b)
		}
	}
	c.Keys.Command = valid
	if c.Keys.Prefix != "" {
		if chord, err := ParseChord(c.Keys.Prefix); err != nil || chord.Prefix || chord.Indexed {
			c.warn("keys.prefix", "invalid prefix key")
			c.Keys.Prefix = ""
		}
	}
	if _, _, _, err := c.SidebarBounds(); err != nil {
		c.warn("ui.sidebar_min_width", err.Error())
		c.UI.SidebarMinWidth = nil
		c.UI.SidebarMaxWidth = nil
	}
	for _, panel := range []struct {
		name string
		p    *SidebarPanel
	}{{"agents", &c.UI.Sidebar.Agents}, {"spaces", &c.UI.Sidebar.Spaces}} {
		one := Config{}
		if panel.name == "agents" {
			one.UI.Sidebar.Agents = *panel.p
		} else {
			one.UI.Sidebar.Spaces = *panel.p
		}
		if err := one.ValidateSidebar(); err != nil {
			c.warn("ui.sidebar."+panel.name, err.Error())
			*panel.p = SidebarPanel{}
		}
	}
	if c.Experimental.KittyGraphics {
		c.warn("experimental.kitty_graphics", "unsupported: image rendering is not implemented")
	}
}
