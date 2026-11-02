package ansi

import (
	"bytes"
	"strings"
	"testing"
)

// A bounded data buffer is allocated only when a string sequence needs it, so
// each terminal no longer pins its full payload limit (4MB per pane in ikesu).
func TestParserBoundedDataGrowsOnDemand(t *testing.T) {
	const limit = 4 * 1024 * 1024
	var got []string
	p := NewParser()
	p.SetDataSize(limit)
	p.SetHandler(Handler{HandleOsc: func(cmd int, data []byte) { got = append(got, string(data)) }})
	if cap(p.data) != 0 {
		t.Fatalf("idle parser holds %d bytes", cap(p.data))
	}

	p.Parse([]byte("\x1b]2;title\x07"))
	if len(got) != 1 || got[0] != "2;title" {
		t.Fatalf("small payload = %q", got)
	}
	if cap(p.data) > 256 {
		t.Fatalf("small payload grew buffer to %d", cap(p.data))
	}

	// Exactly the limit is kept; bytes beyond it are dropped as before.
	big := "52;c;" + strings.Repeat("A", limit)
	p.Parse([]byte("\x1b]" + big + "\x07"))
	if len(got) != 2 || len(got[1]) != limit || got[1] != big[:limit] {
		t.Fatalf("large payload length %d, want %d", len(got[1]), limit)
	}

	// The next sequence releases a buffer grown past the shrink size.
	p.Parse([]byte("\x1b]2;next\x07"))
	if got[2] != "2;next" || cap(p.data) > 256 {
		t.Fatalf("after large payload: %q cap %d", got[2], cap(p.data))
	}
}

// Streaming the same bytes in pieces must deliver the same payloads as the
// eager buffer did, including DCS and APC sequences split across reads.
func TestParserBoundedDataSplitAcrossReads(t *testing.T) {
	input := []byte("a\x1b]8;;https://example.com\x1b\\b\x1bP1$qm\x1b\\c\x1b_Gf=24;" + strings.Repeat("x", 3000) + "\x1b\\d")
	collect := func(chunk int) []string {
		var out []string
		p := NewParser()
		p.SetDataSize(4096)
		p.SetHandler(Handler{
			HandleOsc: func(cmd int, data []byte) { out = append(out, "osc:"+string(data)) },
			HandleDcs: func(cmd Cmd, params Params, data []byte) { out = append(out, "dcs:"+string(data)) },
			HandleApc: func(data []byte) { out = append(out, "apc:"+string(data)) },
			Print:     func(r rune) { out = append(out, string(r)) },
		})
		for i := 0; i < len(input); i += chunk {
			p.Parse(input[i:min(len(input), i+chunk)])
		}
		return out
	}
	want := collect(len(input))
	if len(want) < 7 {
		t.Fatalf("unexpected events %q", want)
	}
	for _, chunk := range []int{1, 2, 7, 255, 256, 257} {
		if got := collect(chunk); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("chunk %d differs:\n got %q\nwant %q", chunk, got, want)
		}
	}
}

// DecodeSequence shares the parser buffer and must apply the same limit.
func TestDecodeSequenceBoundedDataGrows(t *testing.T) {
	p := NewParser()
	p.SetDataSize(16)
	seq := []byte("\x1b]2;" + strings.Repeat("z", 40) + "\x07")
	_, _, n, _ := DecodeSequence(seq, NormalState, p)
	if n != len(seq) {
		t.Fatalf("consumed %d of %d", n, len(seq))
	}
	if !bytes.Equal(p.Data(), seq[2:18]) {
		t.Fatalf("data = %q", p.Data())
	}
}
