package vt

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func assertRuntimeASCIIEqual(t *testing.T, got, want *Emulator) {
	t.Helper()
	if dumpEmulator(got) != dumpEmulator(want) || !reflect.DeepEqual(got.scr.buf, want.scr.buf) ||
		!reflect.DeepEqual(got.Scrollback().Lines(), want.Scrollback().Lines()) || got.Scrollback().Bytes() != want.Scrollback().Bytes() ||
		got.parser.State() != want.parser.State() || !bytes.Equal(got.partial, want.partial) {
		t.Fatalf("dump %t buffer %t history %t bytes %d/%d parser %d/%d partial %q/%q\ngot %s\nwant %s", dumpEmulator(got) == dumpEmulator(want), reflect.DeepEqual(got.scr.buf, want.scr.buf), reflect.DeepEqual(got.Scrollback().Lines(), want.Scrollback().Lines()), got.Scrollback().Bytes(), want.Scrollback().Bytes(), got.parser.State(), want.parser.State(), got.partial, want.partial, dumpEmulator(got), dumpEmulator(want))
	}
	if got.scr.runtimeASCII || len(got.scr.runtimeBlankBounds) != 0 {
		t.Fatal("call-local blank knowledge survived")
	}
}

func TestRuntimeASCIIMatchesWrites(t *testing.T) {
	for _, width := range []int{1, 8, 160} {
		for _, height := range []int{1, 5} {
			for _, budget := range []int{-1, 0, 10000} {
				for _, prefix := range []string{"", "\x1b[31m", "\x1b[44m", "\x1b]8;;https://example.test\x1b\\", "\x1b[1;3;4m", "\x1b[4h", "\x1b[?7l", "\x1b[20h", "\x1b[?1049h", "\x1b[2;4r", "\x1b(0", "\x1b[3G", "\x1b["} {
					t.Run(fmt.Sprintf("%dx%d/%d/%q", width, height, budget, prefix), func(t *testing.T) {
						got, want := NewEmulator(width, height), NewEmulator(width, height)
						defer got.Close()
						defer want.Close()
						for _, e := range []*Emulator{got, want} {
							e.SetScrollbackSize(3)
							if budget >= 0 {
								e.Scrollback().SetMaxBytes(budget)
							}
							_, _ = e.Write([]byte(prefix))
						}
						ga, wa := got.CellAt(width-1, 0), want.CellAt(width-1, 0)
						for _, text := range []string{strings.Repeat(strings.Repeat("a", width-1)+"\r\n \r\n\r\n", 8), strings.Repeat("b\r\n", 12), "x\r\n\t\r\n", "界\r\n", "\x1b[0m\r\n", "\r", "\n", strings.Repeat("z", width) + "\r\n"} {
							got.scr.ClearTouched()
							want.scr.ClearTouched()
							n, err := got.WriteRuntimeASCII([]byte(text))
							wn := 0
							var we error
							for i := range len(text) {
								k, err := want.Write([]byte(text[i : i+1]))
								wn += k
								we = err
							}
							if n != wn || err != we {
								t.Fatal("write result differs")
							}
							assertRuntimeASCIIEqual(t, got, want)
							// Previously obtained pointers may refer to recycled rows.
							*ga, *wa = uv.Cell{Content: "!", Width: 1, Link: uv.Link{URL: "alias"}}, uv.Cell{Content: "!", Width: 1, Link: uv.Link{URL: "alias"}}
						}
					})
				}
			}
		}
	}
}

func TestRuntimeASCIIKeepsMutatingCallbacks(t *testing.T) {
	for _, kind := range []string{"drop", "scroll", "control"} {
		got, want := NewEmulator(8, 2), NewEmulator(8, 2)
		for _, e := range []*Emulator{got, want} {
			e.Scrollback().SetMaxBytes(0)
			mutate := func() { *e.CellAt(7, 0) = uv.Cell{Content: "!", Width: 1} }
			switch kind {
			case "drop":
				e.Scrollback().OnDrop = mutate
			case "scroll":
				e.SetScrollCallback(func(_, _, _, _, _ int) { mutate() })
			case "control":
				e.registerCcHandler('\r', func() bool { mutate(); return false })
			}
		}
		text := []byte(strings.Repeat("row\r\n", 20))
		_, _ = got.WriteRuntimeASCII(text)
		_, _ = want.Write(text)
		assertRuntimeASCIIEqual(t, got, want)
		got.Close()
		want.Close()
	}
}

func TestRuntimeASCIIWithoutHistory(t *testing.T) {
	for _, height := range []int{1, 5} {
		got, want := NewEmulator(8, height), NewEmulator(8, height)
		for _, e := range []*Emulator{got, want} {
			e.SetScrollbackSize(0)
			_, _ = e.Write([]byte("initial\r\n"))
		}
		text := []byte(strings.Repeat("a\r\n \r\n\r\n", 8))
		_, _ = got.WriteRuntimeASCII(text)
		_, _ = want.Write(text)
		assertRuntimeASCIIEqual(t, got, want)
		got.Close()
		want.Close()
	}
}

func TestRuntimeASCIIClosed(t *testing.T) {
	e := NewEmulator(8, 2)
	e.Close()
	if n, err := e.WriteRuntimeASCII([]byte("row\r\n")); n != 0 || err != io.ErrClosedPipe {
		t.Fatalf("closed write = %d, %v", n, err)
	}
}

func TestRuntimeASCIIOwnedCursorAndResize(t *testing.T) {
	got, want := NewEmulator(8, 5), NewEmulator(8, 5)
	defer got.Close()
	defer want.Close()
	var recorded uv.Position
	calls := 0
	got.SetCallbacks(Callbacks{CursorPosition: func(_, p uv.Position) {
		recorded = p
		calls++
		if got.scr.runtimeASCII {
			t.Fatal("cursor observer called inside blank knowledge lifetime")
		}
	}})
	for _, width := range []int{8, 13, 3} {
		got.Resize(width, 5)
		want.Resize(width, 5)
		text := []byte("a\r\nb\r\n")
		before := calls
		old := got.CursorPosition()
		_, _ = got.WriteRuntimeASCII(text)
		_, _ = want.Write(text)
		assertRuntimeASCIIEqual(t, got, want)
		expected := before
		if old != got.CursorPosition() {
			expected++
		}
		if recorded != got.CursorPosition() || calls != expected {
			t.Fatal("owned cursor position not reconciled")
		}
		before = calls
		_, _ = got.Write([]byte("x\r\n"))
		_, _ = want.Write([]byte("x\r\n"))
		if calls <= before+1 {
			t.Fatal("ordinary Write lost movement callbacks")
		}
		assertRuntimeASCIIEqual(t, got, want)
	}
}
