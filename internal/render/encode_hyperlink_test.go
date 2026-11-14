package render

import (
	"bytes"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

func linkedGrid(text string, link uv.Link) [][]uv.Cell {
	row := make([]uv.Cell, 0, len(text))
	for _, r := range text {
		row = append(row, uv.Cell{Content: string(r), Width: 1, Link: link})
	}
	return [][]uv.Cell{row}
}

// A pane's OSC 8 link reaches the host terminal on the cells that carry it,
// and the frame ends with no link open.
func TestFrameForwardsPaneHyperlink(t *testing.T) {
	link := uv.NewLink("https://github.com/kazuph/ikesu/pull/138", "id=pr138")
	f := NewFrame(30, 2)
	f.Text(0, 0, "see ", uv.Style{})
	f.CopyGrid(4, 0, linkedGrid("issue #138", link))
	f.Text(14, 0, " end", uv.Style{})

	data := (&Encoder{}).Frame(f, 0, 0, false)
	open := "\x1b]8;id=pr138;https://github.com/kazuph/ikesu/pull/138\x07"
	if got := bytes.Count(data, []byte(open)); got != 1 {
		t.Fatalf("link opened %d times, want 1: %q", got, data)
	}
	if last := bytes.LastIndex(data, []byte("\x1b]8;")); !bytes.HasPrefix(data[last:], []byte("\x1b]8;;\x07")) {
		t.Fatalf("frame leaves a link open: %q", data)
	}

	emu := vt.NewEmulator(30, 2)
	defer emu.Close()
	if _, err := emu.Write(data); err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 18; x++ {
		got := emu.CellAt(x, 0)
		linked := x >= 4 && x < 14
		if linked && got.Link.URL != link.URL || !linked && got.Link.URL != "" {
			t.Fatalf("cell %d %q link %q, linked=%v", x, got.Content, got.Link.URL, linked)
		}
	}
}

// Frames without links stay byte-identical to the output before links were
// forwarded: no OSC 8 sequence at all.
func TestFrameWithoutLinksEmitsNoHyperlink(t *testing.T) {
	e := &Encoder{}
	a, b := NewFrame(20, 2), NewFrame(20, 2)
	a.Text(0, 0, "https://plain.test/x", uv.Style{})
	b.Text(0, 0, "https://plain.test/y", uv.Style{})
	for _, f := range []*Frame{a, b} {
		if data := e.Frame(f, 0, 0, false); bytes.Contains(data, []byte("\x1b]8;")) {
			t.Fatalf("unexpected OSC 8: %q", data)
		}
	}
}

// Changing only the link of a cell rewrites it, and removing the link
// rewrites it without one.
func TestFrameDiffRewritesLinkChanges(t *testing.T) {
	e := &Encoder{}
	plain := NewFrame(10, 1)
	plain.Text(0, 0, "abc", uv.Style{})
	linked := NewFrame(10, 1)
	linked.CopyGrid(0, 0, linkedGrid("abc", uv.NewLink("https://a.test/")))

	e.Frame(plain, 0, 0, false)
	if data := e.Frame(linked, 0, 0, false); !bytes.Contains(data, []byte("\x1b]8;;https://a.test/\x07abc\x1b]8;;\x07")) {
		t.Fatalf("link added without rewrite: %q", data)
	}
	data := e.Frame(plain, 0, 0, false)
	if !bytes.Contains(data, []byte("abc")) || bytes.Contains(data, []byte("https://a.test/")) {
		t.Fatalf("link removal not rewritten: %q", data)
	}
}

// Links that could end the OSC sequence early or inject other sequences into
// the host terminal are dropped; the text is still drawn.
func TestFrameDropsUnsafeHyperlinks(t *testing.T) {
	for name, link := range map[string]uv.Link{
		"escape":     uv.NewLink("https://a.test/\x1b[31m"),
		"bel":        uv.NewLink("https://a.test/\x07"),
		"c1":         uv.NewLink("https://a.test/\u009c"),
		"params":     uv.NewLink("https://a.test/", "id=a;b"),
		"invalid":    uv.NewLink("https://a.test/\xff"),
		"too long":   uv.NewLink("https://a.test/" + strings.Repeat("a", maxHyperlinkBytes)),
		"params bel": uv.NewLink("https://a.test/", "id=\x07"),
	} {
		f := NewFrame(10, 1)
		f.CopyGrid(0, 0, linkedGrid("abc", link))
		data := (&Encoder{}).Frame(f, 0, 0, false)
		if bytes.Contains(data, []byte("\x1b]8;")) || !bytes.Contains(data, []byte("abc")) {
			t.Fatalf("%s: %q", name, data)
		}
	}
}

// A frame that ends on a linked cell still closes the link, so the next
// frame's plain cells carry none.
func TestFrameLinkDoesNotLeakIntoNextFrame(t *testing.T) {
	e := &Encoder{}
	a := NewFrame(6, 1)
	a.Text(0, 0, "ab", uv.Style{})
	a.CopyGrid(3, 0, linkedGrid("xyz", uv.NewLink("https://a.test/")))
	b := NewFrame(6, 1)
	b.Text(0, 0, "AB", uv.Style{})
	b.CopyGrid(3, 0, linkedGrid("xyz", uv.NewLink("https://a.test/")))

	emu := vt.NewEmulator(6, 1)
	defer emu.Close()
	for _, f := range []*Frame{a, b} {
		data := e.Frame(f, 0, 0, false)
		if _, err := emu.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if got := emu.CellAt(0, 0); got.Content != "A" || got.Link.URL != "" {
		t.Fatalf("plain cell after a linked frame: %+v", got)
	}
	data := e.Frame(b, 0, 0, false)
	if bytes.Contains(data, []byte("\x1b]8;")) {
		t.Fatalf("unchanged frame rewrote links: %q", data)
	}
}

// Adjacent different links switch directly; a wide linked glyph keeps its
// link; drawing over a linked cell with Set or Box drops the link.
func TestFrameLinkSwitchWideAndOverlay(t *testing.T) {
	f := NewFrame(12, 3)
	f.CopyGrid(0, 0, linkedGrid("ab", uv.NewLink("https://a.test/")))
	f.CopyGrid(2, 0, linkedGrid("cd", uv.NewLink("https://b.test/")))
	f.CopyGrid(0, 1, [][]uv.Cell{{{Content: "界", Width: 2, Link: uv.NewLink("https://w.test/")}, {Width: 0}}})
	f.CopyGrid(0, 2, linkedGrid("overlay", uv.NewLink("https://o.test/")))
	f.Set(1, 2, "#", uv.Style{})
	f.Box(3, 2, 3, 1, uv.Style{})

	data := (&Encoder{}).Frame(f, 0, 0, false)
	if !bytes.Contains(data, []byte("\x1b]8;;https://a.test/\x07ab\x1b]8;;https://b.test/\x07cd")) {
		t.Fatalf("adjacent links did not switch directly: %q", data)
	}
	emu := vt.NewEmulator(12, 3)
	defer emu.Close()
	if _, err := emu.Write(data); err != nil {
		t.Fatal(err)
	}
	if got := emu.CellAt(0, 1); got.Content != "界" || got.Link.URL != "https://w.test/" {
		t.Fatalf("wide linked glyph: %+v", got)
	}
	for x, want := range []string{"https://o.test/", "", "https://o.test/"} {
		if got := emu.CellAt(x, 2).Link.URL; got != want {
			t.Fatalf("overlay row cell %d link %q want %q", x, got, want)
		}
	}
}
