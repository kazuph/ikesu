package client

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

// 縦長の画面では、手元と別の機械のどちらでも、右サイドバーの描画・クリック領域が無く、
// 広がったペインの右端へのクリックと PTY の列数が表示と一致する。

func (h *rightMachineHarness) resizeAll(size tui.Winsize, portrait bool) {
	h.t.Helper()
	h.m.size = size
	for _, e := range h.m.endpoints {
		e.connection.enqueue(resizeMessage(size))
	}
	h.until(fmt.Sprintf("resize %+v portrait=%t", size, portrait), func() bool {
		for _, e := range h.m.endpoints {
			s := e.snapshot.State
			if s.Cols != size.Cols || s.Rows != size.Rows || s.Portrait != portrait {
				return false
			}
		}
		return true
	})
	h.paintNow()
}

func (h *rightMachineHarness) paintNow() {
	h.t.Helper()
	h.m.outputFence.Accept([]byte("\x1b[0n"))
	if err := h.m.paint(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *rightMachineHarness) rightHits() (n int) {
	for _, hit := range h.m.sidebar.Hits {
		if hit.Right {
			n++
		}
	}
	return n
}

func (h *rightMachineHarness) activate(id string) {
	h.t.Helper()
	if h.m.active == id {
		return
	}
	for _, hit := range append([]render.MachineHit(nil), h.m.sidebar.Hits...) {
		if hit.Machine != id || hit.Right || hit.Header || hit.Row.Kind != render.RowWorkspace {
			continue
		}
		if err := h.m.input(mouseEvent(h.t, fmt.Sprintf("\x1b[<0;%d;%dM", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
			h.t.Fatal(err)
		}
		if err := h.m.input(mouseEvent(h.t, fmt.Sprintf("\x1b[<0;%d;%dm", hit.Rect.X+1, hit.Rect.Y+1))); err != nil {
			h.t.Fatal(err)
		}
		h.until("switch to "+id, func() bool { return h.m.pending == nil && h.m.active == id })
		h.m.suppressRelease = false
		h.paintNow()
		return
	}
	h.t.Fatal("no workspace row for machine", id)
}

// portraitPaneText joins the visible text of a pane's grid.
func portraitPaneText(in *render.Input, pid int) string {
	var text strings.Builder
	if p := in.Panes[pid]; p != nil {
		for _, row := range p.Grid {
			for _, c := range row {
				text.WriteString(c.Content)
			}
			text.WriteString("\n")
		}
	}
	return text.String()
}

func TestPortraitTwoMachinesRightHiddenClicksAndPTY(t *testing.T) {
	h := newRightMachineHarness(t)
	m := h.m
	landscape := tui.Winsize{Cols: 160, Rows: 35, Xpixel: 1600, Ypixel: 900}
	portrait := tui.Winsize{Cols: 160, Rows: 35, Xpixel: 1600, Ypixel: 1801} // 行と列は同じ、ピクセルだけ縦長
	h.resizeAll(landscape, false)
	if h.rightHits() == 0 {
		t.Fatal("landscape lost right machine hits")
	}
	h.resizeAll(portrait, true)
	for round, id := range []string{"remote", "local", "remote"} {
		h.activate(id)
		e := m.endpoints[id]
		s := e.snapshot.State
		if s.RightSidebarArea().W != 0 || h.rightHits() != 0 {
			t.Fatalf("%s: portrait right=%+v hits=%d", id, s.RightSidebarArea(), h.rightHits())
		}
		frame := render.ComposeClient(*e.snapshot, &m.sidebar, "test", false).Frame
		for y := 0; y < frame.H; y++ {
			if c := frame.Cell(frame.W-1, y); c != nil && strings.Contains(c.Content, "[") {
				t.Fatalf("%s: right sidebar text drawn at the right edge y=%d", id, y)
			}
		}
		// 広がったペインの右端（横長なら右サイドバーだった列）をクリックすると、そのペインが選ばれる。
		ws := s.ActiveWorkspace()
		rects := s.PaneRectsForDisplay(ws.TabRef(), s.PanesArea())
		right := rects[0]
		for _, r := range rects {
			if r.Rect.X+r.Rect.W > right.Rect.X+right.Rect.W {
				right = r
			}
		}
		if right.Rect.X+right.Rect.W != s.Cols {
			t.Fatalf("%s: right pane does not reach the right edge: %+v cols=%d", id, right.Rect, s.Cols)
		}
		x, y := s.Cols-3, right.Rect.Y+right.Rect.H/2
		if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))); err != nil {
			t.Fatal(err)
		}
		if err := m.input(mouseEvent(t, fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))); err != nil {
			t.Fatal(err)
		}
		h.until(fmt.Sprintf("%s: click at the widened right edge", id), func() bool {
			return e.snapshot.State.FocusedPane() == right.ID
		})
		m.suppressRelease = false
		// そのペインの kernel PTY の列数が、表示上のペイン内側の幅と一致する。
		w, hgt := model.PaneTermSize(right.Rect, s.PaneBorders)
		marker := fmt.Sprintf("PORTRAIT_PTY_%d_", round)
		if err := m.sendInput(id, []byte(fmt.Sprintf("printf '%s%%s_\\n' \"$(stty size | tr ' ' x)\"\r", marker))); err != nil {
			t.Fatal(err)
		}
		pattern := regexp.MustCompile(marker + `([0-9]+)x([0-9]+)_`)
		h.until(fmt.Sprintf("%s: pty size printed", id), func() bool {
			return pattern.FindStringSubmatch(portraitPaneText(e.snapshot, right.ID)) != nil
		})
		got := pattern.FindStringSubmatch(portraitPaneText(e.snapshot, right.ID))
		if got[1] != fmt.Sprint(hgt) || got[2] != fmt.Sprint(w) {
			t.Fatalf("%s: pty rows=%s cols=%s want %dx%d (rect %+v)", id, got[1], got[2], hgt, w, right.Rect)
		}
	}

	// ピクセルが取れない端末：最初は行数で横長、端末の応答が遅れて届くと縦長へ。
	unknown := tui.Winsize{Cols: 160, Rows: 35}
	h.resizeAll(unknown, false)
	if h.rightHits() == 0 {
		t.Fatal("pixel-less landscape lost right hits")
	}
	if err := m.input(mouseEvent(t, "\x1b[4;1801;1600t")); err != nil {
		t.Fatal(err)
	}
	active := m.endpoints[m.active]
	h.until("late pixel reply reaches the active machine", func() bool { return active.snapshot.State.Portrait })
	h.paintNow()
	if h.rightHits() != 0 || active.snapshot.State.RightSidebarArea().W != 0 {
		t.Fatal("late pixel reply left the right sidebar on the active machine")
	}

	h.resizeAll(landscape, false)
	if h.rightHits() == 0 {
		t.Fatal("landscape did not restore right hits")
	}
}
