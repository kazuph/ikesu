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
					c.Warnings = append(c.Warnings, invalidType("keys."+name, value, "string or array of strings"))
				} else {
					valid := true
					for _, spec := range bindings {
						if spec != "" {
							if _, err := ParseChord(spec); err != nil {
								c.Warnings = append(c.Warnings, fmt.Sprintf("invalid keybinding: keys.%s = %q; using default binding", name, spec))
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
	c.applyIndexedBindings()
	if binding, legacy := c.Keys.Actions["fullscreen"]; legacy {
		if _, explicit := c.Keys.Actions["zoom"]; !explicit {
			c.Keys.Actions["zoom"] = binding
		}
	}
	c.validateValues()
	c.validateCompat()
	c.unsupportedValues()
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
			continue
		}
		decodeItem(raw[name], dst.Field(i), key, warnings)

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
	c.validateSidebarItems()
	if c.Experimental.KittyGraphics {
		c.warn("experimental.kitty_graphics", "unsupported: image rendering is not implemented")
	}
}

func decodeItem(raw any, dst reflect.Value, key string, warnings *[]string) bool {
	custom := dst.CanAddr() && dst.Addr().Type().Implements(reflect.TypeOf((*interface{ UnmarshalTOML(any) error })(nil)).Elem())
	if !custom {
		switch dst.Kind() {
		case reflect.Struct:
			table, ok := raw.(map[string]any)
			if !ok {
				*warnings = append(*warnings, invalidType(key, raw, "a table"))
				return false
			}
			decodeTable(table, dst, key, warnings)
			return true
		case reflect.Map:
			table, ok := raw.(map[string]any)
			if !ok {
				*warnings = append(*warnings, invalidType(key, raw, "a table"))
				return false
			}
			dst.Set(reflect.MakeMap(dst.Type()))
			names := make([]string, 0, len(table))
			for name := range table {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				value := reflect.New(dst.Type().Elem()).Elem()
				if decodeItem(table[name], value, key+"."+name, warnings) {
					dst.SetMapIndex(reflect.ValueOf(name), value)
				}
			}
			return true
		case reflect.Slice:
			list := reflect.ValueOf(raw)
			if !list.IsValid() || list.Kind() != reflect.Slice {
				*warnings = append(*warnings, invalidType(key, raw, "an array"))
				return false
			}
			dst.Set(reflect.MakeSlice(dst.Type(), 0, list.Len()))
			for i := 0; i < list.Len(); i++ {
				value := reflect.New(dst.Type().Elem()).Elem()
				if decodeItem(list.Index(i).Interface(), value, fmt.Sprintf("%s[%d]", key, i), warnings) {
					dst.Set(reflect.Append(dst, value))
				}
			}
			return true
		}
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(map[string]any{"value": raw}); err != nil {
		*warnings = append(*warnings, key+": "+err.Error())
		return false
	}
	box := reflect.New(reflect.StructOf([]reflect.StructField{{Name: "Value", Type: dst.Type(), Tag: reflect.StructTag(`toml:"value"`)}}))
	if _, err := toml.Decode(encoded.String(), box.Interface()); err != nil {
		*warnings = append(*warnings, invalidType(key, raw, expectedType(dst.Type())))
		return false
	}
	dst.Set(box.Elem().Field(0))
	return true
}

func expectedType(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		return expectedType(t.Elem())
	}
	switch t.Kind() {
	case reflect.Bool:
		return "a boolean"
	case reflect.String:
		return "a string"
	case reflect.Int, reflect.Int64:
		return "an integer"
	case reflect.Slice:
		return "an array"
	case reflect.Struct, reflect.Map:
		return "a table"
	}
	return "a valid value"
}

func invalidType(key string, raw any, expected string) string {
	return fmt.Sprintf("config parse error: %s = %#v; invalid type, expected %s; using default", key, raw, expected)
}
func (c Config) LegacyRestoreEnabled() bool {
	return c.AgentRestore.Enabled != nil && *c.AgentRestore.Enabled
}
