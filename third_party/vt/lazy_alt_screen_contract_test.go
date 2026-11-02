package vt

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Resize notifications drive the caller's row metadata and image positions,
// so a lazy alternate screen must report exactly what an eager one did.
func TestLazyAlternateResizeCallbacksMatchEager(t *testing.T) {
	for _, reflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("reflow%v", reflow), func(t *testing.T) {
			record := func(e *Emulator, log *[]string) {
				e.SetCallbacks(Callbacks{Resize: func(alt bool, oldWidth, height, pushed, pulled int) {
					*log = append(*log, fmt.Sprint(alt, oldWidth, height, pushed, pulled))
				}})
			}
			lazy, eager := NewEmulator(20, 8), NewEmulator(20, 8)
			defer lazy.Close()
			defer eager.Close()
			eager.scrs[1] = *NewScreen(20, 8)
			var lazyLog, eagerLog []string
			record(lazy, &lazyLog)
			record(eager, &eagerLog)
			resize := func(w, h int) {
				for _, e := range []*Emulator{lazy, eager} {
					if reflow {
						e.ResizeWithReflow(w, h, nil)
					} else {
						e.Resize(w, h)
					}
				}
			}
			write := func(s string) {
				for _, e := range []*Emulator{lazy, eager} {
					_, _ = e.WriteString(s)
				}
			}
			write("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\nseven\r\neight\r\nnine")
			resize(12, 5) // shrink before first use
			resize(30, 9) // grow before first use
			resize(16, 6)
			write("\x1b[?1049hALT\x1b[?1049l") // first use
			resize(10, 4)
			resize(22, 7)
			if !reflect.DeepEqual(lazyLog, eagerLog) || len(lazyLog) == 0 {
				t.Fatalf("resize notifications differ:\nlazy  %v\neager %v", lazyLog, eagerLog)
			}
		})
	}
}

// Only a fully initial, inactive alternate screen of the main screen's size
// may be dropped on import; any other state must be kept and re-exported.
func TestLazyAlternateHandoffKeepsNonInitialScreens(t *testing.T) {
	export := func(e *Emulator) terminalTransfer {
		t.Helper()
		b, err := e.MarshalHandoff()
		if err != nil {
			t.Fatal(err)
		}
		var v terminalTransfer
		if err = gob.NewDecoder(bytes.NewReader(b)).Decode(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	encode := func(v terminalTransfer) []byte {
		var b bytes.Buffer
		if err := gob.NewEncoder(&b).Encode(v); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	base := NewEmulator(20, 6)
	defer base.Close()
	_, _ = base.WriteString("main")
	initial := export(base)
	cases := []struct {
		name   string
		mutate func(*screenTransfer)
		keep   bool
	}{
		{"initial", func(*screenTransfer) {}, false},
		{"size", func(s *screenTransfer) {
			*s = transferScreen(NewScreen(15, 4))
		}, true},
		{"saved cursor", func(s *screenTransfer) { s.Saved.X = 3 }, true},
		{"scroll region", func(s *screenTransfer) { s.Scroll = uv.Rect(0, 1, 20, 3) }, true},
		{"history", func(s *screenTransfer) {
			s.History = append(s.History, uv.Line{{Content: "h", Width: 1}})
		}, true},
		{"history limit", func(s *screenTransfer) { s.HistoryLimit++ }, true},
		{"cell", func(s *screenTransfer) {
			s.Cells[0] = append(uv.Line(nil), s.Cells[0]...)
			s.Cells[0][0] = uv.Cell{Content: "x", Width: 1}
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := initial
			alt := transferScreen(NewScreen(20, 6))
			c.mutate(&alt)
			v.Screens[1] = alt
			e := NewEmulator(1, 1)
			defer e.Close()
			if err := e.UnmarshalHandoff(encode(v)); err != nil {
				t.Fatal(err)
			}
			if kept := e.scrs[1].buf != nil; kept != c.keep {
				t.Fatalf("alternate screen kept=%v, want %v", kept, c.keep)
			}
			if again := export(e); !reflect.DeepEqual(again.Screens[1], alt) {
				t.Fatal("alternate screen state changed across import and export")
			}
		})
	}
}
