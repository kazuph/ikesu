package config

import "testing"

func BenchmarkStage2ParseChord(b *testing.B) {
	for i := 0; i < b.N; i++ {
		chord, err := ParseChord("prefix+?")
		if err != nil || chord.Key.Code != '?' {
			b.Fatal(chord, err)
		}
	}
}
