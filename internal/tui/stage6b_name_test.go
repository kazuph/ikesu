package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestStage6bNameDeletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  uv.Key
		want string
	}{
		{"ctrl-u", uv.Key{Code: 'u', Mod: uv.ModCtrl}, ""},
		{"cmd-backspace", uv.Key{Code: uv.KeyBackspace, Mod: uv.ModSuper}, ""},
		{"ctrl-backspace", uv.Key{Code: uv.KeyBackspace, Mod: uv.ModCtrl}, "日本語 alpha-"},
		{"alt-backspace", uv.Key{Code: uv.KeyBackspace, Mod: uv.ModAlt}, "日本語 alpha-"},
		{"ctrl-h", uv.Key{Code: 'h', Mod: uv.ModCtrl}, "日本語 alpha-"},
		{"ctrl-w", uv.Key{Code: 'w', Mod: uv.ModCtrl}, "日本語 alpha-"},
		{"backspace", uv.Key{Code: uv.KeyBackspace}, "日本語 alpha-beta "},
		{"ctrl-c", uv.Key{Code: 'c', Mod: uv.ModCtrl}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{}
			value := "日本語 alpha-beta  "
			a.dialog = &uiDialog{input: &value}
			a.dialogKey(tc.key)
			if value != tc.want {
				t.Fatalf("got %q, want %q", value, tc.want)
			}
		})
	}
	a := &App{}
	value := "12"
	a.dialog = &uiDialog{input: &value, replaceDefault: true}
	a.dialogKey(uv.Key{Code: uv.KeyBackspace})
	if value != "" || a.dialog.replaceDefault {
		t.Fatalf("default remains: %q", value)
	}
}
