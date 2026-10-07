package render

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

// screenText runs the encoded frame through a real VT emulator and returns
// the resulting screen as plain text (what a user's terminal would show).
func screenText(t *testing.T, out *Output, w, h int) string {
	t.Helper()
	enc := &Encoder{}
	data := enc.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible)
	emu := vt.NewEmulator(w, h)
	defer emu.Close()
	if _, err := emu.Write(data); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	var b strings.Builder
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			if c := emu.CellAt(x, y); c != nil && c.Width > 0 {
				sb.WriteString(c.Content)
			}
		}
		if y > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(sb.String(), " "))
	}
	return b.String()
}

func testState(w, h int) *model.AppState {
	s := model.New("/tmp", w, h)
	return s
}

func blankSnap() *PaneSnap {
	return &PaneSnap{Label: "shell", Status: StatusGlyph{Key: "unknown", Rune: "·", Color: Dim}}
}

func TestRenderExpandedSidebarFooter(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	text := screenText(t, out, 80, 24)
	for _, want := range []string{"[menu]", "[new]", "«"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestRenderCollapsedSidebar(t *testing.T) {
	s := testState(80, 24)
	s.SidebarVisible = false
	pid := s.Workspaces[0].Layout.Focused()
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	text := screenText(t, out, 80, 24)
	if !strings.Contains(text, "»") {
		t.Fatalf("missing » in:\n%s", text)
	}
	if strings.Contains(text, "[menu]") {
		t.Fatalf("collapsed sidebar must not show footer:\n%s", text)
	}
}

func TestRenderMenu(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	out := Compose(Input{
		State: s,
		Panes: map[int]*PaneSnap{pid: blankSnap()},
		Menu: &Menu{
			X: 10, Y: 3,
			Items: []MenuItem{
				{Label: "Split vertical"},
				{Label: "Split horizontal"},
				{Separator: true},
				{Label: "Close pane"},
			},
			Hover: -1, Sel: -1,
		},
	})
	text := screenText(t, out, 80, 24)
	for _, want := range []string{"Split vertical", "Split horizontal", "Close pane"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if out.MenuRect.W <= 0 || len(out.MenuItemRows) != 4 {
		t.Fatalf("menu rect rows = %v", out.MenuItemRows)
	}
}

func TestRenderDialog(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	out := Compose(Input{
		State:  s,
		Panes:  map[int]*PaneSnap{pid: blankSnap()},
		Dialog: &Dialog{Title: "Quit goterrace?", Confirm: "quit", Cancel: "cancel"},
	})
	text := screenText(t, out, 80, 24)
	for _, want := range []string{"Quit goterrace?", "quit", "cancel"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestRenderScrollIndicator(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	snap := blankSnap()
	snap.ScrollOff = 12
	snap.ScrollN = 50
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}, Scroll: map[int]int{pid: 12}})
	text := screenText(t, out, 80, 24)
	if !strings.Contains(text, "↑12") {
		t.Fatalf("missing scroll indicator in:\n%s", text)
	}
}

func TestRenderStatusCountsAndButtons(t *testing.T) {
	s := testState(100, 24)
	pid := s.Workspaces[0].Layout.Focused()
	snap := blankSnap()
	snap.Status = StatusGlyph{Key: "working", Rune: "◐", Color: Yellow}
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}})
	text := screenText(t, out, 100, 24)
	for _, want := range []string{"◐ 1", "[split │]", "[split ─]", "[close]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestRenderSelectionHighlight(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	snap := blankSnap()
	r := innerRectFor(t, s, pid)
	grid := make([][]uv.Cell, r.H)
	for y := range grid {
		grid[y] = make([]uv.Cell, r.W)
		for x := range grid[y] {
			grid[y][x] = uv.Cell{Content: "a", Width: 1}
		}
	}
	snap.Grid = grid
	sel := &Selection{Pane: pid, FromX: 0, FromY: 0, ToX: 3, ToY: 0}
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}, Sel: sel})
	// The selected cells must carry reverse video.
	c := out.Frame.Cell(r.X, r.Y)
	if c == nil || c.Style.Attrs&uv.AttrReverse == 0 {
		t.Fatalf("selected cell not reversed: %+v", c)
	}
	out2 := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}})
	c2 := out2.Frame.Cell(r.X, r.Y)
	if c2 == nil || c2.Style.Attrs&uv.AttrReverse != 0 {
		t.Fatalf("unselected cell reversed: %+v", c2)
	}
}

// innerRectFor mirrors the model's pane inner-rect calculation (rect minus
// its 1-cell border).
func innerRectFor(t *testing.T, s *model.AppState, pid int) layout.Rect {
	t.Helper()
	ws := s.ActiveWorkspace()
	if ws == nil {
		t.Fatal("no workspace")
	}
	for _, r := range ws.Layout.Panes(s.PanesArea()) {
		if r.ID == pid {
			return layout.Rect{X: r.Rect.X + 1, Y: r.Rect.Y + 1, W: r.Rect.W - 2, H: r.Rect.H - 2}
		}
	}
	t.Fatalf("pane %d not found", pid)
	return layout.Rect{}
}

func TestEncoderSyncWrapsAndAnchor(t *testing.T) {
	s := testState(80, 24)
	pid := s.Workspaces[0].Layout.Focused()
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	enc := &Encoder{}
	data := string(enc.Frame(out.Frame, 3, 4, true))
	if !strings.HasPrefix(data, "\x1b[?2026h") {
		t.Fatalf("missing sync start: %q", data[:20])
	}
	if !strings.Contains(data, "\x1b[?25l") {
		t.Fatal("missing cursor hide")
	}
	if !strings.Contains(data, "\x1b[?2026l") {
		t.Fatal("missing sync end")
	}
	// Anchor CUP for row 5 col 4 (1-based) appears after syncEnd too.
	tail := data[strings.Index(data, "\x1b[?2026l"):]
	if !strings.Contains(tail, "\x1b[5;4H") {
		t.Fatalf("anchor not repeated outside sync: %q", tail)
	}
	if !strings.HasSuffix(tail, "\x1b[?25h") {
		t.Fatalf("cursor not restored: %q", tail)
	}
}

// scribble fills every cell of f with the marker rune, imitating pane
// content behind a popup.
func scribble(f *Frame, marker string) {
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			f.Set(x, y, marker, uv.Style{})
		}
	}
}

// noMarker asserts that no cell inside (exclusive of) rect r still carries
// the marker left over from the background.
func noMarker(t *testing.T, f *Frame, r layout.Rect, marker string) {
	t.Helper()
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		for x := r.X + 1; x < r.X+r.W-1; x++ {
			if c := f.Cell(x, y); c != nil && c.Content == marker {
				t.Fatalf("background %q leaked through popup at (%d,%d)", marker, x, y)
			}
		}
	}
}

func TestMenuInteriorIsOpaque(t *testing.T) {
	f := NewFrame(60, 20)
	scribble(f, "@")
	out := &Output{}
	m := &Menu{X: 10, Y: 4, Hover: -1, Sel: -1, Items: []MenuItem{
		{Label: "Split vertical"},
		{Label: "Split horizontal"},
		{Separator: true},
		{Label: "Close pane"},
	}}
	drawMenu(f, m, out)
	noMarker(t, f, MenuRect(m, 60, 20), "@")
}

func TestDialogInteriorIsOpaque(t *testing.T) {
	f := NewFrame(60, 20)
	scribble(f, "@")
	out := &Output{}
	d := &Dialog{Title: "Quit goterrace?", Detail: "Stops all panes.", Confirm: "quit", Cancel: "cancel"}
	drawDialog(f, d, out)
	outer, _, _ := DialogRects(d, 60, 20)
	noMarker(t, f, outer, "@")
}

func TestDialogButtonsFullyDrawn(t *testing.T) {
	d := &Dialog{Title: "Close workspace?", Detail: "Closes workspace 1.", Confirm: "close", Cancel: "cancel"}
	f := NewFrame(80, 24)
	out := &Output{}
	drawDialog(f, d, out)
	_, cancel, confirm := DialogRects(d, 80, 24)

	rowText := func(r layout.Rect) string {
		var sb strings.Builder
		for x := r.X; x < r.X+r.W; x++ {
			if c := f.Cell(x, r.Y); c != nil {
				sb.WriteString(c.Content)
			}
		}
		return sb.String()
	}
	if got, want := rowText(cancel), "[ cancel  esc ]"; got != want {
		t.Fatalf("cancel row = %q, want %q", got, want)
	}
	if got, want := rowText(confirm), "[ close  ↵ ]"; got != want {
		t.Fatalf("confirm row = %q, want %q", got, want)
	}
	// Hit-test rects must cover the whole drawn string.
	if cancel.W != len([]rune("[ cancel  esc ]")) {
		t.Fatalf("cancel.W = %d, want %d", cancel.W, len([]rune("[ cancel  esc ]")))
	}
}
