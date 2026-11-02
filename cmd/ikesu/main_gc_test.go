package main

import (
	"os"
	"runtime/debug"
	"testing"
)

func TestConfigureServerGC(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   bool
		value string
		want  int
	}{
		{name: "default", want: 50},
		{name: "explicit", set: true, value: "200", want: 200},
		{name: "explicit off", set: true, value: "off", want: -1},
		{name: "explicit empty", set: true, value: "", want: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := debug.SetGCPercent(100)
			defer debug.SetGCPercent(old)
			if tc.set {
				t.Setenv("GOGC", tc.value)
				if tc.value == "200" {
					debug.SetGCPercent(200)
				}
				if tc.value == "off" {
					debug.SetGCPercent(-1)
				}
			} else {
				t.Setenv("GOGC", "")
				if err := os.Unsetenv("GOGC"); err != nil {
					t.Fatal(err)
				}
			}
			configureServerGC()
			if got := debug.SetGCPercent(-1); got != tc.want {
				t.Fatalf("GC percent = %d, want %d", got, tc.want)
			}
		})
	}
}
