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
