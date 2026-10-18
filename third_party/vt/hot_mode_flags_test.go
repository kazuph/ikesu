package vt

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHotModeFlagsMatchNegotiatedModes(t *testing.T) {
	e := NewEmulator(8, 4)
	check := func() {
		t.Helper()
		for _, v := range []struct {
			mode ansi.Mode
			got  bool
		}{{ansi.ModeAutoWrap, e.autoWrap}, {ansi.ModeOrigin, e.originMode}, {ansi.ModeLineFeedNewLine, e.newLineMode}} {
			if v.got != e.ModeSet(v.mode) {
				t.Fatalf("mode %v cached=%t actual=%t", v.mode, v.got, e.ModeSet(v.mode))
			}
		}
	}
	check()
	callbacks := 0
	e.SetCallbacks(Callbacks{EnableMode: func(ansi.Mode) { callbacks++; check() }, DisableMode: func(ansi.Mode) { callbacks++; check() }})
	for _, m := range []ansi.Mode{ansi.ModeAutoWrap, ansi.ModeOrigin, ansi.ModeLineFeedNewLine} {
		for _, v := range []ansi.ModeSetting{ansi.ModeSet, ansi.ModeReset, ansi.ModePermanentlySet, ansi.ModePermanentlyReset} {
			e.setMode(m, v)
			check()
		}
	}
	for _, seq := range []string{"\x1b[?7h\x1b[?6h\x1b[20h", "\x1b[?7l\x1b[?6l\x1b[20l", "\x1bc", "\x1b[?7l\x1b[?6h\x1b[20h", "\x1bc"} {
		if _, err := e.Write([]byte(seq)); err != nil {
			t.Fatal(err)
		}
		check()
	}
	if callbacks == 0 {
		t.Fatal("callbacks missing")
	}
}
