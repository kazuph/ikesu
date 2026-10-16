package tui

import (
	"io"
	"testing"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage7ParkedSearchResize(t *testing.T) {
	for _, viewer := range []bool{false, true} {
		for _, rows := range []int{35, 25} {
			name := "direct"
			if viewer {
				name = "viewer"
			}
			if rows == 25 {
				name += "-height"
			}
			t.Run(name, func(t *testing.T) {
				teststate.Lock(t)
				a := specRuntimeApp(t)
				first := a.focusedPane()
				a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
				other := a.focusedPane()
				a.exec(model.CmdFocusPane{Pane: first})
				inner := a.innerRect(first)
				p, err := a.sup.SpawnWith(first, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 80 ]; do printf 'ROW-%03d %080d alpha\\n' $i $i; i=$((i+1)); done; printf 'PARKED_READY\\n'; exec /bin/cat"})
				if err != nil {
					t.Fatal(err)
				}
				specWaitText(t, p, "PARKED_READY")
				a.enterCopy(false)
				specFeed(a, "/alpha\r")
				before := *a.cm
				distance := a.bufferLen(p) - a.copyInnerH() - a.cm.top
				a.exec(model.CmdFocusPane{Pane: other})
				ui := a
				var v *Viewer
				if viewer {
					v = a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
					v.app.paneCopies, a.paneCopies = a.paneCopies, nil
					ui = v.app
					v.Resize(a, 90, rows, 0, 0)
				} else {
					a.exec(model.CmdResize{Cols: 90, Rows: rows})
				}
				parked := ui.paneCopies[first]
				if parked == nil || parked.query != "alpha" || len(parked.matches) != 0 || parked.matchIdx != -1 {
					t.Fatalf("parked resize retained stale search: %+v", parked)
				}
				check := func(ui *App) {
					ui.exec(model.CmdFocusPane{Pane: first})
					if ui.cm.cx != before.cx || ui.cm.cy != before.cy {
						t.Fatal("resize moved invalid search cursor")
					}
					if ui.bufferLen(p)-ui.copyInnerH()-ui.cm.top != distance {
						t.Fatal("parked reflow lost distance from end")
					}
					ui.copyNextMatch(1)
					if len(ui.cm.matches) != 80 || ui.cm.matchIdx < 0 {
						t.Fatalf("repeat search: %+v", ui.cm)
					}
					point := ui.cm.matches[ui.cm.matchIdx].Start
					lines := p.BufferLines()
					text := ""
					for _, c := range lines[point.Row][point.Col : point.Col+len("alpha")] {
						text += c.Content
					}
					if text != "alpha" || ui.cm.cx != point.Col || ui.cm.cy != point.Row {
						t.Fatalf("repeat at stale coordinate: %q", text)
					}
				}
				if viewer {
					v.with(a, check)
				} else {
					check(a)
				}
			})
		}
	}
}
