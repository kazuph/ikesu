package config

import (
	uv "github.com/charmbracelet/ultraviolet"
	"testing"
)

func TestStage2ChordAliases(t *testing.T) {
	for _, pair := range [][2]string{{"command+y", "cmd+y"}, {"meta+y", "alt+y"}, {"hyper+y", "hyper+y"}, {"prefix+return", "prefix+enter"}, {"prefix+bs", "prefix+backspace"}, {"prefix+period", "prefix+."}, {"prefix+slash", "prefix+/"}, {"prefix+backslash", "prefix+\\"}, {"prefix+quote", "prefix+'"}, {"prefix+semicolon", "prefix+;"}, {"prefix+colon", "prefix+:"}} {
		t.Run(pair[0], func(t *testing.T) {
			got, err := ParseChord(pair[0])
			if err != nil {
				t.Fatal(err)
			}
			if pair[0] == "hyper+y" {
				if got.Key.Mod != uv.ModHyper {
					t.Fatal(got)
				}
				return
			}
			want, err := ParseChord(pair[1])
			if err != nil || got != want {
				t.Fatal(got, want, err)
			}
		})
	}
}

func TestStage2ShiftedSymbols(t *testing.T) {
	for _, key := range []uv.Key{{Code: '/', ShiftedCode: '?', Mod: uv.ModShift}, {Code: '?', Mod: uv.ModShift}, {Code: '?'}} {
		c, err := ParseChord("prefix+?")
		if err != nil || !c.Match(key, true) {
			t.Fatal(key, err)
		}
	}
	c, _ := ParseChord("cmd+shift+1..9")
	for i, code := range "!@#$%^&*(" {
		key := uv.Key{Code: code, Mod: uv.ModSuper}
		if !c.Match(key, false) || c.IndexCode(key) != rune('1'+i) {
			t.Fatal(key)
		}
	}
	if c.Match(uv.Key{Code: '1', Mod: uv.ModSuper}, false) {
		t.Fatal("unshifted key matched")
	}
}

func TestStage2ShiftedBaseAndSymbol(t *testing.T) {
	for _, tc := range []struct {
		name, base, symbol string
		code, shifted      rune
		mod                uv.KeyMod
		prefix             bool
	}{
		{"slash", "prefix+shift+slash", "prefix+?", '/', '?', uv.ModShift, true},
		{"digit", "cmd+shift+1", "cmd+!", '1', '!', uv.ModSuper | uv.ModShift, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, _ := ParseChord(tc.base)
			symbol, _ := ParseChord(tc.symbol)
			for _, input := range []struct {
				name         string
				key          uv.Key
				base, symbol bool
			}{
				{"base", uv.Key{Code: tc.code, Mod: tc.mod}, true, false},
				{"alternate", uv.Key{Code: tc.code, ShiftedCode: tc.shifted, Mod: tc.mod}, true, true},
				{"symbol", uv.Key{Code: tc.shifted, Mod: tc.mod}, false, true},
			} {
				t.Run(input.name, func(t *testing.T) {
					if got := base.Match(input.key, tc.prefix); got != input.base {
						t.Fatalf("base binding: got %v want %v", got, input.base)
					}
					if got := symbol.Match(input.key, tc.prefix); got != input.symbol {
						t.Fatalf("symbol binding: got %v want %v", got, input.symbol)
					}
				})
			}
		})
	}
}
