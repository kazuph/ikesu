package vt

import (
	"sync"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestReflowSnapshotCursorSupportsConcurrentReadOrder(t *testing.T) {
	shape := newReflowShape(uv.Line{{Content: "a", Width: 1}, {Content: "b", Width: 1}, {Content: "c", Width: 1}})
	var root *reflowIndex
	for i := 0; i < DefaultScrollbackSize/10; i++ {
		root = root.append(shape)
	}
	view := ReflowSnapshot{history: reflowProjection{index: root, width: 2, rows: root.cost(2).rows, cursor: &reflowCursor{}}}
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(offset int) {
			defer workers.Done()
			for i := 0; i < view.HistoryLen(); i++ {
				row := (i + offset) % view.HistoryLen()
				flag := view.HistoryRow(row)
				want := ReflowRow{Continued: row%2 == 1, Width: 2 - row%2}
				if flag != want {
					t.Errorf("row=%d got=%v want=%v", row, flag, want)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
}
