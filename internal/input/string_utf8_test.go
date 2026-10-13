package input

import "testing"

func TestUTF8StringSequenceLength(t *testing.T) {
	for _, prefix := range []string{"\x1b]9;", "\x1b]777;notify;", "\x1bP>|", "\x1b_", "\x1bX", "\x1b^", "\x9d9;", "\x90>|", "\x98", "\x9e", "\x9f"} {
		for _, end := range []string{"\a", "\x1b\\"} {
			stream := []byte(prefix + "本 ✳ 応答本文" + end)
			for split := 1; split < len(stream); split++ {
				if n, complete := seqLen(stream[:split]); n != 0 || complete {
					t.Fatalf("prefix %q end %q split %d: n=%d complete=%v", prefix, end, split, n, complete)
				}
			}
			if n, complete := seqLen(append(stream, 'x')); n != len(stream) || !complete {
				t.Fatalf("prefix %q end %q: n=%d complete=%v", prefix, end, n, complete)
			}
		}
	}
}

func TestUTF8TerminalStringDoesNotLeakInput(t *testing.T) {
	for _, prefix := range []string{"\x1b]0;", "\x1b]9;", "\x1b]777;notify;", "\x1bP>|", "\x1b_", "\x1bX", "\x1b^", "\x9d9;", "\x90>|", "\x98", "\x9e", "\x9f"} {
		for _, end := range []string{"\a", "\x1b\\"} {
			if prefix[0] != 0x9d && (len(prefix) < 2 || prefix[1] != ']') && end == "\a" {
				continue
			}
			sequence := prefix + "本 ✳ 応答本文" + end
			stream := []byte(sequence + "x")
			check := func(chunks [][]byte) {
				t.Helper()
				var f Framer
				var events []Event
				for _, chunk := range chunks {
					events = append(events, f.Push(chunk)...)
				}
				if len(events) != 2 || events[0].Kind != KindUnknown || string(events[0].Raw) != sequence || events[1].Kind != KindKey || events[1].Key.Code != 'x' || f.Pending() {
					t.Fatalf("prefix %q end %q: events=%+v pending=%v", prefix, end, events, f.Pending())
				}
			}
			for split := 0; split <= len(stream); split++ {
				check([][]byte{stream[:split], stream[split:]})
			}
			var chunks [][]byte
			for i := range stream {
				chunks = append(chunks, stream[i:i+1])
			}
			check(chunks)
		}
	}
}
