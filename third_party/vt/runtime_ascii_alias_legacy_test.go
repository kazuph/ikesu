//go:build !flatcells

package vt

import (
	"fmt"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestRuntimeASCIIMatchesWritesLegacyAliases(t *testing.T) {
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
