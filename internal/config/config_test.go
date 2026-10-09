package config

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

// SPEC G6: the key parser resolves percent → '%', double_quote → '"'.
func TestParseKeyBindingPunctuation(t *testing.T) {
	cases := map[string]rune{
		"prefix+percent":      '%',
		"prefix+double_quote": '"',
		"prefix+minus":        '-',
		"prefix+v":            'v',
		"prefix+[":            '[',
		"prefix+]":            ']',
	}
	for spec, want := range cases {
		got, ok := ParseKeyBinding(spec)
		if !ok || got != want {
			t.Errorf("ParseKeyBinding(%q) = %q,%v; want %q,true", spec, got, ok, want)
		}
	}
	if _, ok := ParseKeyBinding("ctrl+x"); ok {
		t.Error("non-prefix spec should be rejected")
	}
	if _, ok := ParseKeyBinding("prefix+ab"); ok {
		t.Error("multi-rune key should be rejected")
	}
}

// Defaults come from the isolated distribution prefix probes.
func TestDefaultKeyLists(t *testing.T) {
	c := Config{}
	if !c.CopyKeys()['['] || c.CopyKeys()[']'] {
		t.Error("default copy_mode must contain only prefix+[")
	}
	if c.SplitVerticalKeys()['%'] || !c.SplitVerticalKeys()['v'] {
		t.Error("default split_vertical must contain only prefix+v")
	}
	if c.SplitHorizontalKeys()['"'] || !c.SplitHorizontalKeys()['-'] {
		t.Error(`default split_horizontal must contain only prefix+minus`)
	}
}

// User [keys] overrides replace the default list entirely (no double
// firing): setting copy_mode to prefix+o removes the bracket bindings.
func TestUserKeyOverride(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "config.toml")
	os.WriteFile(f, []byte(`
[keys]
copy_mode = ["prefix+o"]
split_vertical = ["prefix+|"]
`), 0o644)
	teststate.Setenv(t, EnvVar, f)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.CopyKeys()['o'] || c.CopyKeys()['['] || c.CopyKeys()[']'] {
		t.Errorf("override copy_mode = %v; want only 'o'", c.CopyKeys())
	}
	if !c.SplitVerticalKeys()['|'] || c.SplitVerticalKeys()['v'] || c.SplitVerticalKeys()['%'] {
		t.Errorf("override split_vertical = %v; want only '|'", c.SplitVerticalKeys())
	}
	// Unset key keeps its default.
	if c.SplitHorizontalKeys()['"'] || !c.SplitHorizontalKeys()['-'] {
		t.Error("unset split_horizontal must keep defaults")
	}
}
