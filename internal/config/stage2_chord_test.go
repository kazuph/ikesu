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
