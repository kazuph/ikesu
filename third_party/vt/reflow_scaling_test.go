package vt

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func unindexedHistoryFixture(rows int) *Screen {
	s := NewScreen(160, 45)
	s.scrollback.SetMaxBytes(10000000)
	for i := 0; i < rows; i++ {
		s.scrollback.Push(uv.Line{{Content: "x", Width: 1}})
	}
	s.cur.Y = s.Height() - 1
	return s
}

func indexedHistoryFixture(rows int) (*Screen, ReflowSnapshot) {
	s := unindexedHistoryFixture(rows)
	view := s.reflowIndexed(159, 45, func(int) ReflowRow { return ReflowRow{} }, false, true)
	return s, view
}

func BenchmarkFirstIndexedWidthHistoryScaling(b *testing.B) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				s := unindexedHistoryFixture(rows)
				b.StartTimer()
				s.reflowIndexed(159, 45, func(int) ReflowRow { return ReflowRow{} }, false, true)
			}
		})
	}
}

func BenchmarkPreparedFirstIndexedWidthHistoryScaling(b *testing.B) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				s := unindexedHistoryFixture(rows)
				get := func(int) ReflowRow { return ReflowRow{} }
				s.prepareReflowHistory(get)
				b.StartTimer()
				s.reflowIndexed(159, 45, get, false, true)
			}
		})
	}
}

func indexedNextWidth(s *Screen, view ReflowSnapshot, width int) ReflowSnapshot {
	history := view.HistoryLen()
	return s.reflowIndexed(width, 45, func(y int) ReflowRow {
		if y < history {
			return view.HistoryRow(y)
		}
		if y-history < 45 {
			return view.MainRow(y - history)
		}
		return ReflowRow{}
	}, false, true)
}

func TestRepeatedIndexedWidthRetainsWholeHistoryWithoutRebuildingIt(t *testing.T) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		s, view := indexedHistoryFixture(rows)
		h := s.scrollback.indexed
		middle := h.at(rows / 2)
		for _, width := range []int{160, 159, 160} {
			serial := h.clipping.serial
			view = indexedNextWidth(s, view, width)
			if s.scrollback.indexed != h || h.at(rows/2) != middle || h.clipping.serial != serial+uint64(s.Height()) {
				t.Fatalf("rows=%d width=%d retained=%v created=%d want=%d", rows, width, h.at(rows/2) == middle, h.clipping.serial-serial, s.Height())
			}
			if view.HistoryLen() != rows {
				t.Fatal("history count changed")
			}
		}
	}
}

func BenchmarkRepeatedIndexedWidthHistoryScaling(b *testing.B) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			s, view := indexedHistoryFixture(rows)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				view = indexedNextWidth(s, view, 159+i%2)
			}
		})
	}
}
