package client

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/cellwidth"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func TestMachineNoticeUsesAvailableCellsAndExpiresAtTenSeconds(t *testing.T) {
	for _, cols := range []int{250, 160, 80} {
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
				// Long notices use the freed sidebar footer, leaving the new menu visible.
				x, y, end = 1, 22, max(1, state.SidebarArea().W-1)
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

func TestMachineNoticeKeepsJapaneseCellWidths(t *testing.T) {
	const rows = 25
	const notice = "handoff snapshot をコピーしました（24 行）"
	const decomposed = "か\u3099"
	for _, cols := range []int{250, 80} {
		t.Run(fmt.Sprint(cols), func(t *testing.T) {
			state := model.New(t.TempDir(), cols, rows)
			in := render.Input{State: state}
			path := t.TempDir() + "/terminal"
			out, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			m := &machineClient{active: "local", order: []string{"local"}, out: out, size: tui.Winsize{Cols: cols, Rows: rows}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &in}}}
			emu := vt.NewEmulator(cols, rows)
			defer emu.Close()
			go io.Copy(io.Discard, emu)
			var offset int
			check := func(text, want string, x, y, end int) {
				t.Helper()
				m.notify(text)
				if err := m.paint(); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := emu.Write(data[offset:]); err != nil {
					t.Fatal(err)
				}
				offset = len(data)
				var got strings.Builder
				for col := x; col < end; col++ {
					if cell := emu.CellAt(col, y); cell != nil {
						got.WriteString(cell.Content)
					}
				}
				if got.String() != want {
					t.Fatalf("rendered notice = %q, want %q", got.String(), want)
				}
				if index := strings.Index(want, decomposed); index >= 0 {
					col := x + cellwidth.String(want[:index])
					if cell := emu.CellAt(col, y); cell == nil || cell.Content != decomposed {
						t.Fatalf("combining mark did not stay in its base cell: %+v", cell)
					}
				}
			}
			base := render.Compose(in)
			if cols == 250 {
				bar := base.ActionBar
				x := bar.Copy.X + bar.Copy.W
				check(notice, notice, x, bar.Area.Y, x+cellwidth.String(notice))
				check(decomposed, decomposed, x, bar.Area.Y, x+2)
				return
			}
			end := state.SidebarArea().W - 1
			y := render.SidebarFooter(state).Y
			check(notice, "handoff snapshot をコピ…", 1, y, end)
			prefix := strings.Repeat("a", end-3)
			check(prefix+"あい", prefix+" …", 1, y, end)
			check(prefix+decomposed+"い", prefix+" …", 1, y, end)
			prefix = prefix[:len(prefix)-1]
			check(prefix+decomposed+"い", prefix+decomposed+"…", 1, y, end)
		})
	}
}
