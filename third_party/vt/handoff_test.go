package vt

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHandoffTerminalStateAndPartialSequences(t *testing.T) {
	for _, partial := range []string{"", "\x1b[2;", "\x1b]2;before", "\x1b[31\n;", "\xe7\x8c"} {
		t.Run(partial, func(t *testing.T) {
			a, b := NewEmulator(12, 4), NewEmulator(1, 1)
			defer a.Close()
			defer b.Close()
			a.SetScrollbackSize(100)
			a.Scrollback().SetMaxBytes(4096)
			_, _ = a.WriteString("old1\r\nold2\r\nold3\r\nold4\r\nold5\x1b[?2004h\x1b[?1002h\x1b[?1016h\x1b[>1u\x1b[>5u\x1b[?1049h\x1b[32mALT\x1b7\x1b[2;4H" + partial)
			state, err := a.MarshalHandoff()
			if err != nil {
				t.Fatal(err)
			}
			if err = b.UnmarshalHandoff(state); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []ansi.Mode{ansi.ModeBracketedPaste, ansi.ModeMouseButtonEvent, ansi.ModeMouseExtSgrPixel, ansi.ModeAltScreenSaveCursor} {
				if a.ModeSet(mode) != b.ModeSet(mode) {
					t.Fatalf("mode lost: %v", mode)
				}
			}
			if !b.KbdExplicit() {
				t.Fatal("keyboard stack lost")
			}
			// Complete the same sequence on the original and imported terminals.
			suffix := map[string]string{"": "", "\x1b[2;": "3H", "\x1b]2;before": "-after\a", "\x1b[31\n;": "1m", "\xe7\x8c": "\xab"}[partial]
			for _, e := range []*Emulator{a, b} {
				_, _ = e.WriteString(suffix + "Z\x1b8!")
			}
			if a.Render() != b.Render() || a.CursorPosition() != b.CursorPosition() {
				t.Fatalf("active screen differs: original=%q imported=%q", a.Render(), b.Render())
			}
			for _, e := range []*Emulator{a, b} {
				_, _ = e.WriteString("\x1b[<u\x1b[?1049l")
			}
			if !b.KbdExplicit() {
				t.Fatal("keyboard stack depth lost")
			}
			if a.Render() != b.Render() || !reflect.DeepEqual(a.Scrollback().Lines(), b.Scrollback().Lines()) || a.Scrollback().Bytes() != b.Scrollback().Bytes() {
				t.Fatal("main screen or history differs")
			}
			if a.title != b.title {
				t.Fatal("partial OSC title lost")
			}
		})
	}
}
