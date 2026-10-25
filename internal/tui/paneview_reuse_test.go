package tui

import (
	"io"
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPaneGridRowsShareOnlyUnchangedCellsAndKeepPublishedScreens(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 40, 8, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	write := func(text string) { p.WithEmu(func(e *vt.Emulator) { e.Write([]byte(text)) }) }
	write("\x1b[2J\x1b[H\x1b[31m日本e\u0301\x1b[0m\x1b]8;;https://example.test\x1b\\LINK\x1b]8;;\x1b\\\r\nSECOND")
	first := a.paneGrid(p, 0, 8, 40)
	viewer := a.NewViewer(io.Discard, 106, 38, nil)
	viewer.app.paneGrid(p, 0, 5, 7)
	if len(a.paneGridRows[p]) != 8 || len(viewer.app.paneGridRows[p]) != 5 {
		t.Fatal("viewer inherited another display's projection cache")
	}
	saved := make([][]uv.Cell, len(first))
	for y := range first {
		saved[y] = append([]uv.Cell(nil), first[y]...)
	}
	for _, width := range []int{39, 40, 7, 40} {
		got := a.paneGrid(p, 0, 8, width)
		if &got[0][0] != &first[0][0] || cap(got[0]) != width {
			t.Fatal("unchanged row was copied or exposes hidden columns", width)
		}
	}
	write("\x1b[H\x1b[32mCHANGED\x1b[0m")
	changed := a.paneGrid(p, 0, 8, 39)
	if &changed[0][0] == &first[0][0] || &changed[1][0] != &first[1][0] {
		t.Fatal("changed row was mutated or unchanged row was copied")
	}
	for _, text := range []string{"\r\nTHIRD\r\nFOURTH\r\nFIFTH\r\nSIXTH\r\nSEVENTH\r\nEIGHTH\r\nNINTH", "\x1b[?1049hALT日本", "\x1b[?1049l"} {
		write(text)
		for _, view := range [][3]int{{0, 8, 40}, {3, 5, 7}, {1, 8, 39}, {0, 8, 40}} {
			got := a.paneGrid(p, view[0], view[1], view[2])
			fresh := (&App{sup: a.sup}).paneGrid(p, view[0], view[1], view[2])
			if !reflect.DeepEqual(got, fresh) {
				t.Fatal("reused projection differs from current emulator", text, view)
			}
		}
	}
	if !reflect.DeepEqual(first, saved) {
		t.Fatal("later output/scroll/alternate screen mutated a published screen")
	}
	if a.paneGrid(p, 0, 0, 40) != nil {
		t.Fatal("empty viewport changed its nil-grid contract")
	}
	a.sup.Kill(p.ID)
	replacement, err := a.sup.SpawnWith(p.ID, a.cwd, 40, 8, nil, []string{"/bin/cat"})
	if err != nil {
		t.Fatal(err)
	}
	a.paneGrid(replacement, 0, 8, 40)
	if len(a.paneGridRows) != 1 || a.paneGridRows[p] != nil {
		t.Fatal("closed pane's projection survived pane ID reuse")
	}
}
