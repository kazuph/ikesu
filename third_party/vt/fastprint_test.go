package vt

import (
	"fmt"
	"strings"
	"testing"
)

// dumpEmulator renders every observable part of the emulator state affected
// by printing: all cells, the cursor, the phantom flag, lastChar, and the
// scrollback contents.
func dumpEmulator(e *Emulator) string {
	var b strings.Builder
	x, y := e.scr.CursorPosition()
	fmt.Fprintf(&b, "cursor=%d,%d phantom=%v last=%q gl=%d\n", x, y, e.atPhantom, e.lastChar, e.gl)
	for yy := 0; yy < e.scr.Height(); yy++ {
		for xx := 0; xx < e.scr.Width(); xx++ {
			c := e.scr.CellAt(xx, yy)
			if c == nil {
				b.WriteString("<nil>")
				continue
			}
			fmt.Fprintf(&b, "[%q w%d a%d]", c.Content, c.Width, c.Style.Attrs)
		}
		b.WriteByte('\n')
	}
	sb := e.Scrollback()
	fmt.Fprintf(&b, "scrollback=%d\n", sb.Len())
	for i := 0; i < sb.Len(); i++ {
		for _, c := range sb.Line(i) {
			fmt.Fprintf(&b, "[%q w%d]", c.Content, c.Width)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// runWrites feeds chunks of data into e.
func runWrites(e *Emulator, data []byte, chunk int) {
	for i := 0; i < len(data); i += chunk {
		end := i + chunk
		if end > len(data) {
			end = len(data)
		}
		e.Write(data[i:end])
	}
}

func TestPrintRunEquivalence(t *testing.T) {
	const w, h = 10, 5
	long := strings.Repeat("q", w+3)

	cases := []struct {
		name string
		in   string
	}{
		{"plain", "hello world"},
		{"lines scroll", strings.Repeat("abcdefg\r\n", 20)},
		{"fill exact width", strings.Repeat("x", w) + "\r\nnext"},
		{"overlong autowrap", long + "\r\nafter"},
		{"overlong nowrap", "\x1b[?7l" + long + "end"},
		{"combining mark", "ab́c"},
		{"combining split", "éx éy"},
		{"sgr mid run", "\x1b[1m" + strings.Repeat("b", 3) + "\x1b[0m plain"},
		{"rep after ascii", "a\x1b[4b"},
		{"decstbm scroll", "\x1b[2;4r" + strings.Repeat("s\r\n", 10)},
		{"decstbm cursor inside", "\x1b[2;4r\x1b[3;1f" + strings.Repeat("z", 6)},
		{"decslrm", "\x1b[?69h\x1b[3;8s" + strings.Repeat("m", 9) + "\x1b[?69l"},
		{"wide chars", "ab世cd\r\nef"},
		{"wide at edge", strings.Repeat("a", w-2) + "世\r\n"},
		{"charset drawing", "\x1b(0lqk\x1b(B normal"},
		{"single shift", "\x1bN" + "a"},
		{"del in run", "ab\x7fcd"},
		{"tab in run", "a\tb"},
		{"osc mid text", "a\x1b]0;title\x07bc"},
		{"c0 in run", "a\x00b\x08c"},
		{"ends at edge newline", strings.Repeat("y", w) + "\r\nlast"},
		{"two writes worth", strings.Repeat("p", 2*w+1)},
		{"cursor then text", "\x1b[2;3ftext"},
		{"el then text", "junk\x1b[Ktail"},
	}

	chunks := []int{1, 3, 7, 1 << 20}
	for _, tc := range cases {
		for _, ch := range chunks {
			fast := NewEmulator(w, h)
			slow := NewEmulator(w, h)
			slow.disableFastPrint = true
			runWrites(fast, []byte(tc.in), ch)
			runWrites(slow, []byte(tc.in), ch)
			if got, want := dumpEmulator(fast), dumpEmulator(slow); got != want {
				t.Fatalf("%s chunk=%d mismatch\nfast:\n%s\nslow:\n%s", tc.name, ch, got, want)
			}
		}
	}
}
