package client

import (
	"encoding/base64"
	"testing"

	"github.com/kazuph/goterrace/internal/input"
)

func TestAgentAttachKeys(t *testing.T) {
	var f input.Framer
	k := agentAttachKeys{rows: 24}
	for _, tc := range []struct {
		bytes  string
		kind   string
		data   string
		scroll int
		detach bool
	}{
		{"\x02", "", "", 0, false}, {"\x02", "input", "\x02", 0, false},
		{"x", "input", "x", 0, false}, {"\x1b[5~", "scroll", "", 24, false}, {"\x1b[6~", "scroll", "", -24, false},
		{"\x1b[<64;1;1M", "scroll", "", 1, false}, {"\x1b[<65;1;1M", "scroll", "", -1, false},
		{"\x02", "", "", 0, false}, {"q", "", "", 0, true},
	} {
		events := f.Push([]byte(tc.bytes))
		if len(events) != 1 {
			t.Fatalf("%q: %+v", tc.bytes, events)
		}
		m, detach := k.event(events[0])
		data, _ := base64.StdEncoding.DecodeString(m.Data)
		if m.Type != tc.kind || string(data) != tc.data || m.Rows != tc.scroll || detach != tc.detach {
			t.Fatalf("%q: %+v %q detach=%v", tc.bytes, m, data, detach)
		}
	}
}

func TestAgentAttachPaste(t *testing.T) {
	var f input.Framer
	k := agentAttachKeys{}
	events := f.Push([]byte("\x1b[200~hello\x02q\x1b[201~"))
	var paste bool
	for _, event := range events {
		m, detach := k.event(event)
		if detach {
			t.Fatal("paste detached")
		}
		if m.Type == "paste" {
			b, _ := base64.StdEncoding.DecodeString(m.Data)
			if string(b) != "hello\x02q" {
				t.Fatal(string(b))
			}
			paste = true
		}
	}
	if !paste {
		t.Fatal(events)
	}
}
