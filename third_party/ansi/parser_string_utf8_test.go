package ansi

import (
	"strings"
	"testing"
)

func TestStringC1STIsData(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, body string
	}{
		{"OSC", "\x1b]0;", "✳ 末尾"},
		{"DCS", "\x1bPq", "✳ 末尾"},
		{"APC", "\x1b_", "before\x9cafter"},
		{"SOS", "\x1bX", "before\x9cafter"},
		{"PM", "\x1b^", "before\x9cafter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ends := []string{"\x1b\\"}
			if tc.name == "OSC" {
				ends = append(ends, "\a")
			}
			for _, end := range ends {
				input := []byte(tc.prefix + tc.body + end + "TAIL")
				for split := 0; split <= len(input); split++ {
					p := NewParser()
					var data []string
					var printed strings.Builder
					collect := func(b []byte) { data = append(data, string(b)) }
					p.SetHandler(Handler{
						Print:     func(r rune) { printed.WriteRune(r) },
						HandleOsc: func(_ int, b []byte) { collect(b) },
						HandleDcs: func(_ Cmd, _ Params, b []byte) { collect(b) },
						HandleApc: collect, HandleSos: collect, HandlePm: collect,
					})
					for _, chunk := range [][]byte{input[:split], input[split:]} {
						for _, b := range chunk {
							p.Advance(b)
						}
					}
					want := tc.body
					if tc.name == "OSC" {
						want = "0;" + want
					}
					if len(data) != 1 || data[0] != want || printed.String() != "TAIL" {
						t.Fatalf("end %q split %d: data=%q printed=%q", end, split, data, printed.String())
					}
				}
			}
		})
	}
}

func TestStringBELKeepsExistingTermination(t *testing.T) {
	for _, prefix := range []string{"\x1bPq", "\x1b_", "\x1bX", "\x1b^"} {
		p := NewParser()
		var data []string
		collect := func(b []byte) { data = append(data, string(b)) }
		p.SetHandler(Handler{
			HandleDcs: func(_ Cmd, _ Params, b []byte) { collect(b) },
			HandleApc: collect, HandleSos: collect, HandlePm: collect,
		})
		p.Parse([]byte(prefix + "before\aafter"))
		if len(data) != 0 {
			t.Fatalf("BEL terminated %q: %q", prefix, data)
		}
		p.Parse([]byte("\x1b\\"))
		if len(data) != 1 || data[0] != "before\aafter" {
			t.Fatalf("ESC ST did not terminate %q: %q", prefix, data)
		}
	}
}

func TestDecodeStringC1STIsData(t *testing.T) {
	for _, prefix := range []string{"\x1b]0;", "\x1bPq", "\x1b_", "\x1bX", "\x1b^"} {
		for _, end := range []string{"\x1b\\", "\a"} {
			if prefix != "\x1b]0;" && end == "\a" {
				continue
			}
			input := prefix + "✳ 末\x9c" + end
			for _, decode := range []func(string, byte, *Parser) (string, int, int, byte){DecodeSequence[string], DecodeSequenceWc[string]} {
				p := NewParser()
				seq, width, n, state := decode(input+"TAIL", NormalState, p)
				if seq != input || width != 0 || n != len(input) || state != NormalState {
					t.Fatalf("%q: seq=%q width=%d n=%d state=%d", input, seq, width, n, state)
				}
				want := "✳ 末\x9c"
				if prefix == "\x1b]0;" {
					want = "0;" + want
				}
				if string(p.Data()) != want {
					t.Fatalf("%q: data=%q want=%q", input, p.Data(), want)
				}
			}
		}
	}
}
