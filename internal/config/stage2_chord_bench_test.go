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

func BenchmarkStage2DecodeDefault(b *testing.B) {
	data := []byte("onboarding=false\n")
	for i := 0; i < b.N; i++ {
		cfg, err := Decode(data)
		if err != nil || len(cfg.Warnings) != 0 {
			b.Fatal(cfg.Warnings, err)
		}
	}
}
