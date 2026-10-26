//go:build flatcells

package vt

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"reflect"
	"strings"
	"testing"
)

func TestFlatASCIIHistoryEntryMatchesLegacyAccountingAndDeletion(t *testing.T) {
	for _, limit := range []int{1, 3, 8} {
		for _, budget := range []int{0, scrollbackRowBytes - 1, scrollbackRowBytes, scrollbackRowBytes + 8*scrollbackCellBytes, 8 * (scrollbackRowBytes + 8*scrollbackCellBytes)} {
			got, want := NewScrollback(limit), NewScrollback(limit)
			got.SetMaxBytes(budget)
			want.SetMaxBytes(budget)
			var gotDrops, wantDrops []string
			got.OnDrop = func() { gotDrops = append(gotDrops, fmt.Sprint(got.Len(), got.Bytes(), got.Lines())) }
			want.OnDrop = func() { wantDrops = append(wantDrops, fmt.Sprint(want.Len(), want.Bytes(), want.Lines())) }
			for rep := 0; rep < 3; rep++ {
				for _, text := range []string{"", "x", " leading", "two words", "trailing ", strings.Repeat("x", 16)} {
					line := make(uv.Line, len(text))
					for x, value := range []byte(text) {
						line[x] = uv.Cell{Content: asciiContent[value], Width: 1}
					}
					a := got.pushPlainScreenBytes([]byte(text), len(text))
					b := want.pushBytesWithPlainText(line, true, []byte(text))
					if a != b || got.Bytes() != want.Bytes() || !reflect.DeepEqual(got.Lines(), want.Lines()) || !reflect.DeepEqual(gotDrops, wantDrops) {
						t.Fatalf("limit=%d budget=%d rep=%d input=%q got=%v/%d/%v want=%v/%d/%v", limit, budget, rep, text, a, got.Bytes(), got.Lines(), b, want.Bytes(), want.Lines())
					}
				}
			}
		}
	}
}

func TestFlatASCIIHistoryInputAndReflowMatchLegacyEntry(t *testing.T) {
	for _, prefix := range []string{"", "\x1b[31m", "\x1b[44m", "\x1b[4;58;5;42m", "\x1b]8;;https://example.test\x1b\\", "界e\u0301\U0010eeee\r"} {
		for _, budget := range []int{scrollbackRowBytes, scrollbackRowBytes + 8*scrollbackCellBytes, 8 * (scrollbackRowBytes + 8*scrollbackCellBytes)} {
			got, want := NewEmulator(8, 3), NewEmulator(8, 3)
			for _, e := range []*Emulator{got, want} {
				e.SetScrollbackSize(8)
				e.Scrollback().SetMaxBytes(budget)
				if _, err := e.Write([]byte(prefix)); err != nil {
					t.Fatal(err)
				}
			}
			for rep := 0; rep < 3; rep++ {
				for _, text := range []string{"a\r\n", " \r\n", "\r\n", "abcdefg\r\n", "ab\r\n", "abcdefghijk\r\n", "界e\u0301\r\n"} {
					if _, err := got.WriteRuntimeASCII([]byte(text)); err != nil {
						t.Fatal(err)
					}
					if _, err := want.Write([]byte(text)); err != nil {
						t.Fatal(err)
					}
					assertRuntimeASCIIEqual(t, got, want)
				}
			}
			for _, size := range [][2]int{{5, 4}, {9, 2}, {8, 3}} {
				got.Resize(size[0], size[1])
				want.Resize(size[0], size[1])
				assertRuntimeASCIIEqual(t, got, want)
			}
			got.Close()
			want.Close()
		}
	}
}
