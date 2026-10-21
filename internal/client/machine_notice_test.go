package client

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineNoticeUsesAvailableCellsAndExpiresAtTenSeconds(t *testing.T) {
	for _, cols := range []int{160, 80} {
		state := model.New(t.TempDir(), cols, 25)
		in := render.Input{State: state}
		base := render.Compose(in)
		file, err := os.CreateTemp(t.TempDir(), "terminal")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		m := &machineClient{active: "local", order: []string{"local"}, out: file, size: tui.Winsize{Cols: cols, Rows: 25}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &in}}}
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		initial, err := os.ReadFile(file.Name())
		if err != nil {
			t.Fatal(err)
		}
		underlying := vt.NewEmulator(cols, 25)
		_, _ = underlying.Write(initial)
		for _, text := range []string{"retry", strings.Repeat("R", 40)} {
			before := time.Now()
			m.notify(text)
			deadline := m.noticeUntil
			if deadline.Before(before.Add(10*time.Second)) || deadline.After(time.Now().Add(10*time.Second)) {
				t.Fatal("notice duration")
			}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			emu := vt.NewEmulator(cols, 25)
			if _, err := emu.Write(data); err != nil {
				t.Fatal(err)
			}
			x, y, end := base.ActionBar.Copy.X+base.ActionBar.Copy.W, base.ActionBar.Area.Y, base.ActionBar.Cycle.X
			if end-x < len([]rune(text)) {
				x, y, end = 1, 24, max(1, state.SidebarArea().W-1)
			}
			for index, r := range []rune(text) {
				if x+index >= end {
					break
				}
				cell := emu.CellAt(x+index, y)
				if cell == nil || cell.Content != string(r) {
					t.Fatalf("cols=%d notice cell (%d,%d): %+v", cols, x+index, y, cell)
				}
				if !reflect.DeepEqual(cell.Style.Bg, base.Frame.Cell(x+index, y).Style.Bg) {
					t.Fatal("notice changed background")
				}
			}
			for cy := 0; cy < 25; cy++ {
				for cx := 0; cx < cols; cx++ {
					if cy == y && cx >= x && cx < min(end, x+len([]rune(text))) {
						continue
					}
					if !reflect.DeepEqual(emu.CellAt(cx, cy), underlying.CellAt(cx, cy)) {
						t.Fatalf("notice changed unrelated cell (%d,%d)", cx, cy)
					}
				}
			}
			m.advance(deadline.Add(-time.Nanosecond))
			if m.notice != text {
				t.Fatal("notice removed before ten seconds")
			}
			m.advance(deadline)
			if m.notice != "" || !m.noticeUntil.IsZero() {
				t.Fatal("notice remained at ten seconds")
			}
			if err := m.paint(); err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			emu = vt.NewEmulator(cols, 25)
			_, _ = emu.Write(data)
			for i := x; i < end; i++ {
				if emu.CellAt(i, y).Content != base.Frame.Cell(i, y).Content {
					t.Fatal("expired notice did not restore underlying cells")
				}
			}
		}
	}
}
