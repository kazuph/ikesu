package input

import "testing"

func TestPendingBytesPreservePartialPaste(t *testing.T) {
	var before Framer
	before.Push([]byte("\x1b[200~partial"))
	var after Framer
	after.Push(before.PendingBytes())
	events := after.Push([]byte(" paste\x1b[201~"))
	if len(events) != 1 || events[0].Kind != KindPaste || events[0].Text != "partial paste" {
		t.Fatalf("partial paste changed: %+v", events)
	}
}

func TestPendingBytesPreservePartialEscape(t *testing.T) {
	var before Framer
	before.Push([]byte("\x1b["))
	var after Framer
	after.Push(before.PendingBytes())
	events := after.Push([]byte("A"))
	if len(events) != 1 || string(events[0].Raw) != "\x1b[A" {
		t.Fatalf("partial escape changed: %+v", events)
	}
}
