package input

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPasteWaitsForSplitTerminator(t *testing.T) {
	var f Framer
	f.Push([]byte("\x1b[200~first\n"))
	for _, chunk := range []string{"second\n", "\x1b", "[20", "1"} {
		if events := f.FlushTimeout(); len(events) != 0 {
			t.Fatalf("idle emitted partial paste: %+v", events)
		}
		if events := f.Push([]byte(chunk)); len(events) != 0 {
			t.Fatalf("fragment emitted input: %+v", events)
		}
	}
	events := f.Push([]byte("~x"))
	if len(events) != 2 || events[0].Kind != KindPaste || events[0].Text != "first\nsecond\n" || events[1].Key.Code != 'x' || f.Pending() {
		t.Fatalf("completed paste: %+v pending=%v", events, f.Pending())
	}
}

func TestUTF8WaitsAcrossIdle(t *testing.T) {
	for _, prefix := range []string{"", "\x1b"} {
		t.Run(prefix, func(t *testing.T) {
			var f Framer
			f.Push([]byte(prefix + "\xe6"))
			if events := f.FlushTimeout(); len(events) != 0 {
				t.Fatalf("idle emitted incomplete rune: %+v", events)
			}
			f.Push([]byte("\x97"))
			if events := f.FlushTimeout(); len(events) != 0 {
				t.Fatalf("idle emitted incomplete rune: %+v", events)
			}
			events := f.Push([]byte("\xa5"))
			if len(events) != 1 || events[0].Kind != KindKey || events[0].Key.Code != '日' || string(events[0].Raw) != prefix+"日" {
				t.Fatalf("completed rune: %+v", events)
			}
			if events[0].Key.Mod.Contains(uv.ModAlt) != (prefix != "") {
				t.Fatalf("modifier: %+v", events[0].Key)
			}
		})
	}
}
