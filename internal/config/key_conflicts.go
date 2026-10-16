package config

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Resolve explicit bindings before defaults so a configured chord replaces the
// default owner. Navigation's plain keys belong to a separate input mode.
func (c *Config) resolveKeyBindings() {
	type owner struct {
		name  string
		chord Chord
	}
	var accepted []owner
	var names []string
	for name := range ActionDefaults {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, explicit := range []bool{true, false} {
		for _, name := range names {
			_, set := c.Keys.Actions[name]
			if set != explicit || name == "fullscreen" {
				continue
			}
			var clean Bindings
			for _, spec := range c.ActionBindings(name) {
				if spec == "" {
					continue
				}
				chord, err := ParseChord(spec)
				nav := strings.HasPrefix(name, "navigate_")
				if err == nil && !nav && !chord.Prefix && chord.Key.Mod == 0 && unicode.IsPrint(chord.Key.Code) {
					err = fmt.Errorf("unmodified printable key captures terminal input")
				}
				if err != nil {
					c.warn("keys."+name, err.Error()+"; disabling binding")
					continue
				}
				conflict := ""
				for _, prev := range accepted {
					if strings.HasPrefix(prev.name, "navigate_") != nav {
						continue
					}
					if chordsOverlap(prev.chord, chord) {
						conflict = prev.name
						break
					}
				}
				if conflict != "" {
					if explicit {
						c.warn("keys."+name, "conflicts with keys."+conflict+"; disabling binding")
					}
					continue
				}
				// Custom commands intentionally replace ordinary actions on their chord.
				for _, binding := range c.Keys.Command {
					other, e := ParseChord(binding.Key)
					if e == nil && chordsOverlap(other, chord) {
						conflict = "command"
						break
					}
				}
				if conflict != "" {
					continue
				}
				clean = append(clean, spec)
				accepted = append(accepted, owner{name, chord})
			}
			if len(clean) == 0 {
				clean = Bindings{""}
			}
			c.Keys.Actions[name] = clean
		}
	}
}

func chordsOverlap(a, b Chord) bool {
	if a.Prefix != b.Prefix {
		return false
	}
	if !a.Indexed && !b.Indexed {
		return a.Key == b.Key
	}
	for code := '1'; code <= '9'; code++ {
		key := a.Key
		key.Code = code
		if a.Match(key, a.Prefix) && b.Match(key, b.Prefix) {
			return true
		}
	}
	return false
}
