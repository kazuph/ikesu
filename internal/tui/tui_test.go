package tui

import (
	"testing"

	"github.com/kazuph/goterrace/internal/model"
)

// innerRect must never return a non-positive size: a too-narrow terminal
// (sidebar taking the whole width) used to pass negative dimensions into
// vt.NewEmulator and panic.
func TestInnerRectClampsTinyPanes(t *testing.T) {
	for _, cols := range []int{30, 10, 4, 1} {
		st := model.New("", cols, 5)
		a := &App{st: st}
		pid := st.Workspaces[0].Tabs[0].Layout.Focused()
		r := a.innerRect(pid)
		if r.W < 1 || r.H < 1 {
			t.Fatalf("cols=%d: innerRect %+v has non-positive size", cols, r)
		}
	}
}
