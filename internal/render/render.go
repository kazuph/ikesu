package render

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"image/color"
)

// Palette colors (Catppuccin Mocha-ish).
var (
	Accent  = color.RGBA{0x89, 0xb4, 0xfa, 0xff} // blue
	Dim     = color.RGBA{0x6c, 0x70, 0x86, 0xff} // overlay0
	Text    = color.RGBA{0xcd, 0xd6, 0xf4, 0xff} // text
	Subtle  = color.RGBA{0xa6, 0xad, 0xc8, 0xff} // subtext0
	Surface = color.RGBA{0x31, 0x32, 0x44, 0xff} // surface0
	SurfDim = color.RGBA{0x18, 0x18, 0x25, 0xff} // surface_dim (mantle)
	Red     = color.RGBA{0xf3, 0x8b, 0xa8, 0xff}
	Green   = color.RGBA{0xa6, 0xe3, 0xa1, 0xff}
	Yellow  = color.RGBA{0xf9, 0xe2, 0xaf, 0xff}
	Peach   = color.RGBA{0xfa, 0xb3, 0x87, 0xff}
	Mauve   = color.RGBA{0xcb, 0xa6, 0xf7, 0xff}
)

// StatusGlyph describes how a pane's agent status is drawn. Key is the
// state key (done / working / blocked / idle / unknown) used for counts;
// Rune is the currently displayed symbol (animated for working).
type StatusGlyph struct {
	Key   string
	Rune  string
	Color color.Color
	Bold  bool
}

// CursorInfo is the pane cursor position/visibility used for IME anchoring.
type CursorInfo struct {
	X, Y    int
	Visible bool
}

// PaneSnap is everything render needs to draw one pane.
type PaneSnap struct {
	Grid   [][]uv.Cell // content cells, [row][col], scroll applied
	Label  string      // process/agent name for the border title
	Title  string      // terminal (OSC) title appended to the border title
	Tokens map[string]string
	Agent  string // detected agent id ("" = not identified)
	// Restore is "saved session" / "no saved session" for agent panes,
	// shown right-aligned on the top border (SPEC G1).
	Restore   string
	Status    StatusGlyph
	Focused   bool
	Dead      bool
	Cursor    CursorInfo
	ScrollOff int // scrollback lines above the live screen
	ScrollN   int // total scrollback length
}

// Selection is a linear (anchor→head) range inside one pane's inner rect,
// expressed in pane-content cell coordinates.
type Selection struct {
	Pane int
	// From/To are normalized (From is above-or-equal To).
	FromX, FromY int
	ToX, ToY     int
}

// Menu is a right-click popup.
type Menu struct {
	X, Y  int // requested position (top-left), clamped at draw
	Title string
	Items []MenuItem
	Hover int // index under the pointer; -1 = none
	Sel   int // keyboard-selected item; -1 = none
}

// MenuItem is one menu row; Separator rows draw a rule.
type MenuItem struct {
	Label     string
	Separator bool
	Disabled  bool
}

// Toast is a transient notification box shown at the top-right.
type Toast struct {
	Title string
	Body  string
	Kind  string // "needs_attention" | "finished"
}

// Dialog is a centered confirmation box.
type Dialog struct {
	Title   string
	Detail  string
	Confirm string // e.g. "quit"
	Cancel  string // e.g. "cancel"
	// Lines are extra detail rows below Detail (e.g. the missing-session
	// warning list); at most 10 are drawn, per SPEC's dialog height rule
	// 5 + min(count, 10).
	Lines []string
	// Width pins the dialog width (0 → auto-fit contents).
	Width int
	// Input/InputEdit render the detail row as an editable text field.
	Input     string
	InputEdit bool
}

// Input is everything Compose needs for one frame.
// JobDot is one colored ● shown on a workspace row for a meaningful
// background job (SPEC G2). The snapshot (status filtering and runner
// liveness) is computed upstream — rendering never probes processes.
type JobDot struct {
	Color color.Color
}

type Input struct {
	State           *model.AppState
	SidebarConfig   config.Config
	WorkspaceTokens map[int]map[string]string
	Panes           map[int]*PaneSnap
	Scroll          map[int]int
	// JobDots maps workspace index → job dots drawn on its sidebar row.
	JobDots           map[int][]JobDot
	Jobs              []JobRow
	JobsPanel         bool
	JobScroll         int
	Git               map[int]GitStatus
	WorkspaceNames    map[int]string
	SelectedWorkspace *int
	DropIndex         *int
	DragSection       model.Section
	DraggedWorkspace  int
	Dragging          bool

	Menu   *Menu
	Dialog *Dialog
	Sel    *Selection
	Toast  *Toast

	Spin           int    // working-spinner frame
	Copied         string // flash text, e.g. " copied 12 chars "
	Prefix         bool   // prefix-key pending
	SidebarScroll  int    // workspace rows scrolled off the top
	AgentScroll    int
	MobileSwitcher bool
	MobileScroll   int
	// CopyLabel is the action bar's left button text: " COPY ",
	// " PHONE COPY " or " EXIT COPY " (SPEC G1).
	CopyLabel string
	// Copy is the copy-mode view mark (nil = not in copy mode).
	Copy *CopyMark
	// CopyHint is the copy-mode help row drawn on the pane's bottom
	// frame row (in-layout) or the panes area's last row (fullscreen):
	// " COPY  h/j/k/l … " or the search prompt (SPEC G6).
	CopyHint string
	// Help is the keybind-help overlay (SPEC G6 prefix+? / [menu]→keybinds).
	Help *HelpInput
}

// CopyMark locates the copy-mode cursor in pane-inner cell coords and,
// when Fullscreen, makes the pane cover the whole panes area without a
// frame (SPEC G6).
type CopyMark struct {
	Pane       int
	X, Y       int // cursor in inner cell coords
	Fullscreen bool
	Sel        *Selection
}

// Output is a composed frame plus the cursor anchor for IME positioning.
type Output struct {
	Frame         *Frame
	AnchorX       int
	AnchorY       int
	AnchorVisible bool
	// TabHits maps each tab of the active workspace to its bar rect;
	// NewTabRect is the "+" button. Both feed the mouse layer.
	TabHits    []layout.Rect
	NewTabRect layout.Rect
	// ActionBar is the bottom pane action bar's button hit areas; zero
	// rects mean the button did not fit and must not fire.
	ActionBar ActionBarRects
	// MenuRect/DialogRects/ToastRect let the mouse layer hit-test overlays.
	ToastRect     layout.Rect
	MenuRect      layout.Rect
	MenuItemRows  []layout.Rect // per-item row rects (absolute coords)
	DialogRect    layout.Rect
	DialogConfirm layout.Rect
	// HelpRect lets the mouse layer hit-test the help overlay.
	HelpRect     layout.Rect
	DialogCancel layout.Rect
}

// ActionBarRects are the bottom action bar's clickable regions (SPEC G1).
type ActionBarRects struct {
	Area     layout.Rect
	Copy     layout.Rect
	Cycle    layout.Rect
	Rotate   layout.Rect
	Equalize layout.Rect
}

// HelpRow is one row in the keybind help overlay: either a group header
// or a binding→command pair (SPEC G6).
type HelpRow struct {
	Group bool
	Bind  string
	Cmd   string
}

// HelpInput feeds the keybind help overlay.
type HelpInput struct {
	Rows   []HelpRow
	Scroll int // first visible content row
}

// Compose renders the entire screen into a frame.
func Compose(in Input) *Output {
	s := in.State
	f := NewFrame(s.Cols, s.Rows)
	out := &Output{Frame: f, AnchorX: s.Cols - 1, AnchorY: s.Rows - 1}

	drawSidebar(f, s, in)
	drawTabBar(f, s, out)
	drawPanes(f, s, in, out)
	drawActionBar(f, s, in, out)
	drawStatusLine(f, s, in)
	drawPopup(f, in, out)
	if s.Mobile() {
		drawMobile(f, in, out)
	}
	if in.Menu != nil {
		drawMenu(f, in.Menu, out)
	}
	if in.Dialog != nil {
		drawDialog(f, in.Dialog, out)
	}
	if in.Toast != nil {
		drawToast(f, in.Toast, out)
	}
	if in.Help != nil {
		drawHelp(f, in.Help, out)
	}
	return out
}

// --- tab bar ---

// drawTabBar renders the one-row tab strip over the panes area: " name "
// cells for each tab of the active workspace plus a trailing "+" button.
func drawTabBar(f *Frame, s *model.AppState, out *Output) {
	area := s.TabBarArea()
	if area.W <= 0 {
		return
	}
	f.Fill(area.X, area.Y, area.W, 1, " ", uv.Style{Bg: Surface})
	ws := s.ActiveWorkspace()
	if ws == nil {
		return
	}
	x := area.X
	for i, t := range ws.Tabs {
		name := t.TabLabel()
		if t.Zoomed != 0 {
			name += " Z" // zoomed-tab marker (SPEC G1)
		}
		w := len([]rune(name)) + 2
		if x+w > area.X+area.W-3 {
			w = area.X + area.W - 3 - x
		}
		if w <= 0 {
			out.TabHits = append(out.TabHits, layout.Rect{})
			continue
		}
		sty := uv.Style{Fg: Subtle, Bg: Surface}
		if i == ws.ActiveTab {
			sty = uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: Accent, Attrs: uv.AttrBold}
		}
		f.Text(x, area.Y, " "+truncate(name, max(w-1, 0)), sty, x+w)
		out.TabHits = append(out.TabHits, layout.Rect{X: x, Y: area.Y, W: w, H: 1})
		x += w
	}
	if x+3 <= area.X+area.W {
		f.Text(x, area.Y, " + ", uv.Style{Fg: Subtle, Bg: Surface}, x+3)
		out.NewTabRect = layout.Rect{X: x, Y: area.Y, W: 3, H: 1}
	}
}

// --- panes ---

func drawPanes(f *Frame, s *model.AppState, in Input, out *Output) {
	ws := s.ActiveWorkspace()
	if ws == nil {
		return
	}
	area := s.PanesArea()
	// Fullscreen copy: the pane covers the terminal area with no frame,
	// title, gutter or split border (SPEC G6).
	if in.Copy != nil && in.Copy.Fullscreen {
		if snap := in.Panes[in.Copy.Pane]; snap != nil {
			f.CopyGrid(area.X, area.Y, clipGrid(snap.Grid, area.W, area.H))
			if in.Copy.Sel != nil {
				applySelection(f, area.X, area.Y, area.W, area.H, in.Copy.Sel)
			}
			if in.Copy.X >= 0 && in.Copy.X < area.W && in.Copy.Y >= 0 && in.Copy.Y < area.H {
				drawCopyCursor(f, area.X+in.Copy.X, area.Y+in.Copy.Y)
			}
		}
		drawCopyHint(f, area.X, area.Y+area.H-1, area.W, in.CopyHint)
		return
	}
	t := ws.TabRef()
	if t == nil {
		return
	}
	for _, pr := range t.PaneRects(area) {
		snap := in.Panes[pr.ID]
		inner, gutter := model.PaneContentRect(pr.Rect, s.PaneBorders)
		ix, iy, iw, ih := inner.X, inner.Y, inner.W, inner.H
		if s.PaneBorders && pr.Rect.W > 2 && pr.Rect.H > 2 {
			borderSty := uv.Style{Fg: Dim}
			if pr.IsFocused {
				borderSty = uv.Style{Fg: Accent}
			}
			if snap != nil && snap.Dead {
				borderSty = uv.Style{Fg: Dim, Attrs: uv.AttrFaint}
			}
			f.Box(pr.Rect.X, pr.Rect.Y, pr.Rect.W, pr.Rect.H, borderSty)

			// Title: " ZOOM? %<id> <label> <terminal title> " — hidden
			// below width 4, truncated to pane width-4 with '…'
			// (SPEC G1 pane contract). The agent restore label wins the
			// right edge; the left title yields to it.
			titleStop := pr.Rect.X + pr.Rect.W - 1
			if snap != nil && snap.Restore != "" && snap.Agent != "" && pr.Rect.W >= 34 {
				rl := " " + snap.Restore + " "
				rw := len([]rune(rl))
				titleStop = pr.Rect.X + pr.Rect.W - 1 - rw
				f.Text(titleStop, pr.Rect.Y, rl, borderSty, pr.Rect.X+pr.Rect.W-1)
			}
			if pr.Rect.W > 4 {
				title := paneTitle(pr, snap, t)
				f.Text(pr.Rect.X+1, pr.Rect.Y, " "+title+" ", borderSty, titleStop)
			}

			// Scroll indicator on the top border, left of the restore
			// label's slot.
			if off := in.Scroll[pr.ID]; off > 0 && pr.Rect.W > 10 {
				ind := fmt.Sprintf(" ↑%d ", off)
				w := len([]rune(ind))
				f.Text(titleStop-w, pr.Rect.Y, ind, uv.Style{Fg: Yellow}, titleStop)
			}

			// Gutter: the rightmost inner column hosts the scrollbar;
			// terminal content never reaches it (SPEC G1).
			if gutter > 0 {
				drawGutter(f, inner, snap, in.Scroll[pr.ID])
				iw -= gutter
			}
		}

		// Content.
		if snap != nil {
			f.CopyGrid(ix, iy, clipGrid(snap.Grid, iw, ih))
		}

		// Selection highlight (mouse drag or copy-mode anchor range).
		if in.Sel != nil && in.Sel.Pane == pr.ID {
			applySelection(f, ix, iy, iw, ih, in.Sel)
		}
		if in.Copy != nil && in.Copy.Pane == pr.ID {
			if in.Copy.Sel != nil {
				applySelection(f, ix, iy, iw, ih, in.Copy.Sel)
			}
			// Copy cursor lives strictly inside the pane's inner rect.
			if cx, cy := ix+in.Copy.X, iy+in.Copy.Y; cx >= ix && cx < ix+iw && cy >= iy && cy < iy+ih {
				drawCopyCursor(f, cx, cy)
			}
			// The help row replaces the pane's bottom frame row.
			drawCopyHint(f, pr.Rect.X, pr.Rect.Y+pr.Rect.H-1, pr.Rect.W, in.CopyHint)
		}

		// IME cursor anchor for the focused pane.
		if pr.IsFocused && snap != nil {
			out.AnchorX = ix + snap.Cursor.X
			out.AnchorY = iy + snap.Cursor.Y
			out.AnchorVisible = snap.Cursor.Visible
			if out.AnchorX < ix {
				out.AnchorX = ix
			}
			if out.AnchorY < iy {
				out.AnchorY = iy
			}
			if out.AnchorX >= ix+iw {
				out.AnchorX = ix + iw - 1
			}
			if out.AnchorY >= iy+ih {
				out.AnchorY = iy + ih - 1
			}
		}
	}
}

// paneTitle builds the border title: "ZOOM %<id> <label> <title>" for the
// zoomed focused pane, otherwise "%<id> <label> <title>".
func paneTitle(pr layout.PaneRect, snap *PaneSnap, t *model.Tab) string {
	lbl := label(snap)
	title := fmt.Sprintf("%%%d %s", pr.ID, lbl)
	if snap != nil && snap.Title != "" {
		title += " " + snap.Title
	}
	if t.Zoomed == model.PaneID(pr.ID) {
		title = "ZOOM " + title
	}
	return truncateEllipsis(title, pr.Rect.W-4)
}

// truncateEllipsis cuts s to at most n Unicode characters, replacing the
// tail with a single '…' when it had to shorten.
func truncateEllipsis(s string, n int) string {
	rs := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}

// drawGutter fills the rightmost inner column: '▕' track with '▐' for the
// thumb portion whenever the pane has scrollback (SPEC G1).
func drawGutter(f *Frame, inner layout.Rect, snap *PaneSnap, off int) {
	gx := inner.X + inner.W - 1
	for y := inner.Y; y < inner.Y+inner.H; y++ {
		f.Set(gx, y, "▕", uv.Style{Fg: Dim})
	}
	if snap == nil || snap.ScrollN <= 0 || inner.H <= 0 {
		return
	}
	total := snap.ScrollN + inner.H
	th := inner.H * inner.H / total
	if th < 1 {
		th = 1
	}
	if th > inner.H {
		th = inner.H
	}
	// Thumb top: bottom-aligned at offset 0, top-aligned at max offset.
	track := inner.H - th
	top := inner.Y + track
	if snap.ScrollN > 0 {
		frac := float64(off) / float64(snap.ScrollN)
		top = inner.Y + track - int(frac*float64(track))
	}
	for y := top; y < top+th && y < inner.Y+inner.H; y++ {
		f.Set(gx, y, "▐", uv.Style{Fg: Dim})
	}
}

// drawCopyCursor paints the copy-mode cursor: fg panel_bg, bg accent,
// BOLD (SPEC G1/G6).
func drawCopyCursor(f *Frame, x, y int) {
	c := f.Cell(x, y)
	if c == nil {
		return
	}
	if c.Content == "" {
		c.Content = " "
		c.Width = 1
	}
	c.Style.Fg = Surface
	c.Style.Bg = Accent
	c.Style.Attrs = uv.AttrBold
}

// --- bottom pane action bar (SPEC G1) ---

// drawActionBar draws the one-row bar under the panes: the copy label on
// the left, CYCLE LAYOUT / ROTATE PANES / EQUALIZE right-aligned with
// 1-cell gaps and a 1-cell right margin. Buttons that do not fit are
// neither drawn nor clickable.
func drawActionBar(f *Frame, s *model.AppState, in Input, out *Output) {
	area := s.ActionBarArea()
	if area.W <= 0 {
		return
	}
	out.ActionBar.Area = area
	base := uv.Style{Fg: Dim} // reset bg, overlay0 fg
	btn := uv.Style{Fg: Accent, Attrs: uv.AttrBold, Underline: uv.UnderlineSingle}
	f.Fill(area.X, area.Y, area.W, 1, " ", uv.Style{})

	copyLabel := in.CopyLabel
	if copyLabel == "" {
		copyLabel = " COPY "
	}
	cw := len([]rune(copyLabel))
	if area.X+cw <= area.X+area.W {
		f.Text(area.X, area.Y, copyLabel, base, area.X+area.W)
		out.ActionBar.Copy = layout.Rect{X: area.X, Y: area.Y, W: cw, H: 1}
	}

	labels := []struct {
		text string
		slot *layout.Rect
	}{
		{" CYCLE LAYOUT ", &out.ActionBar.Cycle},
		{" ROTATE PANES ", &out.ActionBar.Rotate},
		{" EQUALIZE ", &out.ActionBar.Equalize},
	}
	total := 1 + len(labels) - 1 // right margin and inter-button gaps
	for _, l := range labels {
		total += len([]rune(l.text))
	}
	x := area.X + area.W - total
	if x < area.X {
		x = area.X // too narrow: left-align and drop what does not fit
	}
	for _, l := range labels {
		w := len([]rune(l.text))
		if x+w > area.X+area.W {
			continue
		}
		f.Text(x, area.Y, l.text, btn, area.X+area.W)
		*l.slot = layout.Rect{X: x, Y: area.Y, W: w, H: 1}
		x += w + 1
	}
}

// drawCopyHint renders the copy-mode help/search row at (x, y); the
// row (the pane's bottom frame row) is cleared first so no border
// remnants show past the text.
func drawCopyHint(f *Frame, x, y, w int, hint string) {
	if hint == "" || w <= 0 {
		return
	}
	f.Fill(x, y, w, 1, " ", uv.Style{})
	f.Text(x, y, hint, uv.Style{Fg: Dim}, x+w)
}

func clipGrid(g [][]uv.Cell, w, h int) [][]uv.Cell {
	if len(g) > h {
		g = g[:h]
	}
	for _, row := range g {
		if len(row) > w {
			clipped := make([][]uv.Cell, len(g))
			for i, row := range g {
				clipped[i] = row[:min(len(row), max(w, 0))]
			}
			return clipped
		}
	}
	return g
}

func applySelection(f *Frame, ix, iy, iw, ih int, sel *Selection) {
	for y := 0; y < ih; y++ {
		var x0, x1 int
		switch {
		case y < sel.FromY || y > sel.ToY:
			continue
		case sel.FromY == sel.ToY:
			x0, x1 = sel.FromX, sel.ToX
		case y == sel.FromY:
			x0, x1 = sel.FromX, iw-1
		case y == sel.ToY:
			x0, x1 = 0, sel.ToX
		default:
			x0, x1 = 0, iw-1
		}
		for x := x0; x <= x1 && x < iw; x++ {
			if c := f.Cell(ix+x, iy+y); c != nil {
				c.Style.Attrs |= uv.AttrReverse
			}
		}
	}
}

// --- status line ---

// StatusButtons returns the clickable rects for the three status buttons
// (split vertical, split horizontal, close) on the status line.
func StatusButtons(cols, rows int) (splitV, splitH, close layout.Rect) {
	y := rows - 1
	if y < 0 {
		return
	}
	close = layout.Rect{X: cols - 7, Y: y, W: 7, H: 1}
	splitH = layout.Rect{X: cols - 7 - 10, Y: y, W: 10, H: 1}
	splitV = layout.Rect{X: cols - 7 - 10 - 10, Y: y, W: 10, H: 1}
	return
}

func drawStatusLine(f *Frame, s *model.AppState, in Input) {
	y := s.Rows - 1
	if y < 0 {
		return
	}
	base := uv.Style{Fg: Subtle, Bg: Surface}
	f.Fill(0, y, s.Cols, 1, " ", base)

	x := f.Text(0, y, " gtx  prefix: C-b", base, s.Cols)
	if in.Prefix {
		f.Text(x, y, "  PREFIX ", uv.Style{Attrs: uv.AttrReverse, Bg: Surface}, s.Cols)
	}
	x = f.Text(x+9, y, fmt.Sprintf("  workspace %d/%d", s.Active+1, len(s.Workspaces)), base, s.Cols)

	// Right side: agent counts + buttons (hidden when too narrow).
	right := s.Cols - 1
	if s.Cols >= 60 {
		sv, sh, cl := StatusButtons(s.Cols, s.Rows)
		btn := uv.Style{Fg: Subtle, Bg: Surface}
		f.Text(sv.X, y, "[split │]", btn, sv.X+sv.W)
		f.Text(sh.X, y, "[split ─]", btn, sh.X+sh.W)
		f.Text(cl.X, y, "[close]", btn, cl.X+cl.W)
		right = sv.X - 1
	}

	// Agent summary counts by state key, ahead of the buttons.
	counts := map[string]int{}
	for _, ws := range s.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if snap := in.Panes[pid]; snap != nil && snap.Status.Key != "" {
					counts[snap.Status.Key]++
				}
			}
		}
	}
	order := []struct {
		key, g string
		c      color.Color
	}{
		{"working", workingRune(in.Spin), Yellow}, {"blocked", "▲", Peach}, {"done", "●", Green}, {"idle", "○", Dim},
	}
	for _, o := range order {
		if n := counts[o.key]; n > 0 {
			txt := fmt.Sprintf("%s %d ", o.g, n)
			right -= len([]rune(txt))
			f.Text(right, y, txt, uv.Style{Fg: o.c, Bg: Surface}, s.Cols-1)
		}
	}
}
