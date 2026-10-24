package vt

import (
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestPreparedHistoryFirstWidthChangeReusesClosedLines(t *testing.T) {
	for _, rows := range []int{DefaultScrollbackSize / 10, DefaultScrollbackSize} {
		s := unindexedHistoryFixture(rows)
		before, charged := s.scrollback.Lines(), s.scrollback.Bytes()
		get := func(int) ReflowRow { return ReflowRow{} }
		s.prepareReflowHistory(get)
		h := s.scrollback.prepared.history
		middle, serial := h.at(rows/2), h.clipping.serial
		if s.scrollback.indexed != nil || !reflect.DeepEqual(s.scrollback.Lines(), before) || s.scrollback.Bytes() != charged {
			t.Fatal("preparation changed physical history")
		}
		view := s.reflowIndexed(159, 45, get, false, true)
		if view.HistoryLen() != rows || s.scrollback.indexed != h || h.at(rows/2) != middle || h.clipping.serial != serial+45 {
			t.Fatal("first width change rebuilt prepared history")
		}
	}
}

func TestPreparedHistoryOversizedOutputReleasesRetiredCells(t *testing.T) {
	s := NewScreen(6, 2)
	s.scrollback.SetMaxBytes(scrollbackRowBytes + scrollbackCellBytes)
	s.scrollback.Push(uv.Line{{Content: "x", Width: 1}})
	get := func(int) ReflowRow { return ReflowRow{} }
	s.prepareReflowHistory(get)
	h := s.scrollback.prepared.history
	s.scrollback.Push(uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}})
	s.prepareReflowHistory(get)
	if h.count != 0 || len(h.pool.refs) != 0 {
		t.Fatal("discarded history retained cells or shapes")
	}
}

func TestPreparedHistoryEvictionAndPullPreserveFirstResize(t *testing.T) {
	old, indexed := NewScreen(6, 2), NewScreen(6, 2)
	for _, s := range []*Screen{old, indexed} {
		s.scrollback.SetMaxBytes(10000000)
		for _, text := range []string{"abcdef", "ghijkl", "mnopqr", "stuvwx", "yzABCD", "EFGHIJ"} {
			var row uv.Line
			for _, r := range text {
				row = append(row, uv.Cell{Content: string(r), Width: 1})
			}
			s.scrollback.Push(row)
		}
		s.cur.Y = 1
	}
	flags := []ReflowRow{{Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {Continued: true, Width: 6}, {Width: 6}, {Continued: true, Width: 6}, {}, {}}
	get := func(y int) ReflowRow {
		if y < len(flags) {
			return flags[y]
		}
		return ReflowRow{}
	}
	indexed.prepareReflowHistory(get)
	for _, s := range []*Screen{old, indexed} {
		s.scrollback.dropOldest()
		copy(s.buf.Line(0), s.scrollback.popLast())
	}
	flags = flags[1:]
	for _, size := range [][2]int{{4, 2}, {8, 3}, {3, 2}} {
		view := indexed.reflowIndexed(size[0], size[1], get, false, true)
		flags = old.referenceStreamingReflow(size[0], size[1], flags, false)
		if !reflect.DeepEqual(old.buf.Lines, indexed.buf.Lines) || !reflect.DeepEqual(old.scrollback.Lines(), indexed.scrollback.Lines()) || old.cur.Position != indexed.cur.Position || old.scrollback.Bytes() != indexed.scrollback.Bytes() {
			t.Fatalf("size=%v output differed", size)
		}
		if !reflect.DeepEqual(flags, view.materialize()) {
			t.Fatal("metadata differed")
		}
	}
}
