package uv

import (
	"reflect"
	"testing"
)

func TestDecoderStringUTF8IsNotC1ST(t *testing.T) {
	const body = "本 ✳ 応答本文"
	for _, prefix := range []string{"\x1b]9;", "\x1b]777;notify;", "\x9d9;", "\x1bP>|", "\x90>|", "\x1b_", "\x9f", "\x1bX", "\x98", "\x1b^", "\x9e"} {
		for _, end := range []string{"\a", "\x1b\\"} {
			osc := prefix[0] == 0x9d || len(prefix) > 1 && prefix[1] == ']'
			if !osc && end == "\a" {
				continue
			}
			sequence := prefix + body + end
			var d EventDecoder
			n, ev := d.Decode([]byte(sequence + "x"))
			if n != len(sequence) {
				t.Fatalf("%q: consumed=%d want=%d event=%#v", sequence, n, len(sequence), ev)
			}
			var want Event
			intro := prefix[0]
			if intro == 0x1b {
				intro = prefix[1]
			}
			switch intro {
			case ']', 0x9d:
				want = UnknownOscEvent(sequence)
			case 'P', 0x90:
				want = TerminalVersionEvent{body}
			case '_', 0x9f:
				want = UnknownApcEvent(sequence)
			case 'X', 0x98:
				want = UnknownSosEvent(sequence)
			default:
				want = UnknownPmEvent(sequence)
			}
			if !reflect.DeepEqual(ev, want) {
				t.Fatalf("%q: event=%#v want=%#v", sequence, ev, want)
			}
		}
	}
}

func TestDecoderNonOSCBELRemainsData(t *testing.T) {
	for _, prefix := range []string{"\x1bP>|", "\x1b_", "\x1bX", "\x1b^"} {
		sequence := prefix + "本\a✳" + "\x1b\\"
		var d EventDecoder
		n, ev := d.Decode([]byte(sequence + "x"))
		if n != len(sequence) {
			t.Fatalf("%q: consumed=%d event=%#v", sequence, n, ev)
		}
		if prefix == "\x1bP>|" && !reflect.DeepEqual(ev, TerminalVersionEvent{"本\a✳"}) {
			t.Fatalf("%q: body=%#v", sequence, ev)
		}
	}
}
