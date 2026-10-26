package vt

import (
	"fmt"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPrintRunTouchedRangeEquivalence(t *testing.T) {
	const width, height = 12, 3
	cases := 0
	for _, column := range []int{0, 3, width - 1} {
		for _, first := range []int{-1, 0, 3, 6, width, width + 1} {
			for _, last := range []int{-1, 0, 3, 6, width, width + 1} {
				for _, length := range []int{0, 1, height} {
					for _, input := range []string{"abc", "XXXd", "abcdefghij", "\x1b[31mchanged\x1b[0m", "\x1b]8;;https://example.com\x1b\\linked", "a世bcd"} {
						fast, slow := NewEmulator(width, height), NewEmulator(width, height)
						slow.disableFastPrint = true
						for _, e := range []*Emulator{fast, slow} {
							e.Write([]byte("abcdefghij\r"))
							e.Write([]byte(fmt.Sprintf("\x1b[1;%dH", column+1)))
							e.scr.buf.Touched = make([]*uv.LineData, length)
							if length > 0 {
								e.scr.buf.Touched[0] = &uv.LineData{FirstCell: first, LastCell: last}
							}
							e.Write([]byte(input + "\r"))
						}
						if !reflect.DeepEqual(screenTestState(fast.scr.buf), screenTestState(slow.scr.buf)) || dumpEmulator(fast) != dumpEmulator(slow) {
							t.Fatalf("column=%d dirty=%d:%d length=%d input=%q", column, first, last, length, input)
						}
						cases++
					}
				}
			}
		}
	}
	t.Logf("cells, styles, links, dirty ranges and cursor matched in %d cases", cases)
}
