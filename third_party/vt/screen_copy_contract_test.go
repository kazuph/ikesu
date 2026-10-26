package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"reflect"
	"testing"
)

func TestStyledDrawingAndCloneUseExplicitScreenWrites(t *testing.T) {
	for _, text := range []string{"a\x1b[31m\u0301z", "\x1b]8;;https://example.test\x1b\\界e\u0301\x1b]8;;\x1b\\", "\x1b[4:3;58:2::19:83:151m👩‍💻\x1b[0m", "value\x1b[?25h"} {
		e := NewEmulator(16, 4)
		reference := uv.NewScreenBuffer(16, 4)
		reference.Method = e.WidthMethod().(ansi.Method)
		styled := uv.NewStyledString(text)
		styled.Draw(e, e.Bounds())
		styled.Draw(reference, reference.Bounds())
		for y := 0; y < e.Height(); y++ {
			for x := 0; x < e.Width(); x++ {
				if got, want := e.CellAt(x, y), reference.CellAt(x, y); !reflect.DeepEqual(got, want) {
					t.Fatalf("text=%q cell=(%d,%d) got=%#v want=%#v", text, x, y, got, want)
				}
			}
		}
		if got, want := screen.CloneArea(e, e.Bounds()), reference.CloneArea(reference.Bounds()); !reflect.DeepEqual(got.Lines, want.Lines) {
			t.Fatalf("cloning CellAt copy lost screen content: %q", text)
		}
		e.Close()
	}
}

func TestScreenStorageReportsActualBackend(t *testing.T) {
	e := NewEmulator(16, 4)
	defer e.Close()
	info := e.ScreenStorage()
	want := 112
	if ExperimentalFlatScreen {
		want = 24
	}
	if info.CellBytes != want || info.CellCapacity < 16*4 || info.PointerFree != ExperimentalFlatScreen {
		t.Fatal("incorrect storage measurement", info)
	}
	e.SetCell(0, 0, &uv.Cell{Content: "e\u0301", Width: 1, Link: uv.Link{URL: "https://example.test/owned"}})
	if ExperimentalFlatScreen && e.ScreenStorage().LiveIDs == 0 {
		t.Fatal("ID storage missing from M6 measurement")
	}
	e.scr.Clear()
	if ExperimentalFlatScreen && e.ScreenStorage().LiveIDs != 0 {
		t.Fatal("clear retained ID storage")
	}
}
