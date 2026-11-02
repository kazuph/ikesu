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
	want := []string{"a", "osc:8;;https://example.com", "b", "dcs:m", "c", "apc:Gf=24;" + strings.Repeat("x", 3000), "d"}
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

func oscPayloads(limit int, input string) []string {
	var got []string
	p := NewParser()
	p.SetDataSize(limit)
	p.SetHandler(Handler{HandleOsc: func(cmd int, data []byte) { got = append(got, string(data)) }})
	p.Parse([]byte(input))
	return got
}

// Payloads at, below, and above a non power-of-two limit keep exactly the
// first limit bytes.
func TestParserBoundedDataLimitEdges(t *testing.T) {
	const limit = 1000
	for _, n := range []int{limit - 1, limit, limit + 1, 3 * limit} {
		body := "2;" + strings.Repeat("q", n-2)
		got := oscPayloads(limit, "\x1b]"+body+"\x07")
		want := body[:min(n, limit)]
		if len(got) != 1 || got[0] != want {
			t.Fatalf("n=%d: got %d bytes, want %d", n, len(got[0]), len(want))
		}
	}
}

// Changing the limit after use must keep the buffer consistent: no panic on
// the next sequence, and a sequence in progress keeps its bytes.
func TestParserSetDataSizeAfterUse(t *testing.T) {
	var got []string
	p := NewParser()
	p.SetHandler(Handler{
		HandleOsc: func(cmd int, data []byte) { got = append(got, string(data)) },
		HandleCsi: func(cmd Cmd, params Params) { got = append(got, "csi") },
	})
	p.Parse([]byte("\x1b]2;x\x07"))
	p.SetDataSize(16)
	p.Parse([]byte("\x1b[0m"))
	p.Parse([]byte("\x1b]2;abc"))
	p.SetDataSize(5)
	p.Parse([]byte("defgh\x07"))
	if strings.Join(got, "|") != "2;x|csi|2;abc" {
		t.Fatalf("got %q", got)
	}
	// Unlimited, then bounded again.
	p.SetDataSize(0)
	p.Parse([]byte("\x1b]2;" + strings.Repeat("u", 100) + "\x07"))
	p.SetDataSize(-1)
	p.SetDataSize(8)
	p.Parse([]byte("\x1b]2;bounded\x07"))
	if last := got[len(got)-1]; last != "2;bounde" || len(got[len(got)-2]) != 102 {
		t.Fatalf("after switching limits: %q", got[len(got)-2:])
	}
}

// A buffer of exactly the shrink size is kept for reuse; a larger one is
// released when the next sequence starts, in Parse and DecodeSequence alike.
func TestParserBoundedDataReleaseThreshold(t *testing.T) {
	for _, size := range []int{dataShrinkSize, dataShrinkSize + 1} {
		p := NewParser()
		p.SetDataSize(4 * 1024 * 1024)
		p.Parse([]byte("\x1b]2;" + strings.Repeat("r", size-2) + "\x07"))
		grown := len(p.data)
		p.Parse([]byte("\x1b]2;s\x07"))
		if released := len(p.data) <= 256; released != (grown > dataShrinkSize) {
			t.Fatalf("parse size %d: grown %d now %d", size, grown, len(p.data))
		}
	}
	for _, start := range []string{"\x1b]", "\x9d"} {
		p := NewParser()
		p.SetDataSize(4 * 1024 * 1024)
		big := []byte(start + "2;" + strings.Repeat("d", 70_000) + "\x07")
		if _, _, n, _ := DecodeSequence(big, NormalState, p); n != len(big) || len(p.Data()) != 70_002 {
			t.Fatalf("decode big: n=%d data=%d", n, len(p.Data()))
		}
		small := []byte(start + "2;e\x07")
		if _, _, n, _ := DecodeSequence(small, NormalState, p); n != len(small) || string(p.Data()) != "2;e" || len(p.data) > 256 {
			t.Fatalf("decode small after big: data %q cap %d", p.Data(), len(p.data))
		}
	}
}
