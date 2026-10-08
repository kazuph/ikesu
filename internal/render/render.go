package render

import (
	"fmt"
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
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
	Grid  [][]uv.Cell // content cells, [row][col], scroll applied
	Label string      // process/agent name for the border title
	Title string      // terminal (OSC) title appended to the border title
	Agent string      // detected agent id ("" = not identified)
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
	State  *model.AppState
	Panes  map[int]*PaneSnap
	Scroll map[int]int
	// JobDots maps workspace index → job dots drawn on its sidebar row.
	JobDots map[int][]JobDot

	Menu   *Menu
	Dialog *Dialog
	Sel    *Selection
	Toast  *Toast

	Spin          int    // working-spinner frame
	Copied        string // flash text, e.g. " copied 12 chars "
	Prefix        bool   // prefix-key pending
	SidebarScroll int    // sidebar rows scrolled off the top
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
			drawCopyCursor(f, area.X+in.Copy.X, area.Y+in.Copy.Y)
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
	total := 1 // right margin
	for _, l := range labels {
		total += len([]rune(l.text)) + 1
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

// --- sidebar ---

// SidebarRowKind classifies scrollable sidebar rows.
type SidebarRowKind int

const (
	// RowWorkspace is a " N <glyph> <name> " line.
	RowWorkspace SidebarRowKind = iota
	// RowAgent is an identified-agent line " <glyph> <pane> <name> ".
	RowAgent
	// RowSep is the "─ agents ─" separator.
	RowSep
)

// SidebarRow is one scrollable sidebar row for drawing and hit-testing.
type SidebarRow struct {
	Kind SidebarRowKind
	Ws   int // workspace index (RowWorkspace / RowAgent)
	Pane int // pane id (RowAgent only)
}

// SidebarRows lists the sidebar's scrollable rows in display order.
func SidebarRows(s *model.AppState, panes map[int]*PaneSnap) []SidebarRow {
	var rows []SidebarRow
	for i := range s.Workspaces {
		if WorkspaceHidden(s, i) {
			continue
		}
		rows = append(rows, SidebarRow{Kind: RowWorkspace, Ws: i, Pane: -1})
	}
	agentRows := false
	for wi, ws := range s.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if snap := panes[pid]; snap != nil && snap.Agent != "" {
					if !agentRows {
						rows = append(rows, SidebarRow{Kind: RowSep, Pane: -1})
						agentRows = true
					}
					rows = append(rows, SidebarRow{Kind: RowAgent, Ws: wi, Pane: pid})
				}
			}
		}
	}
	return rows
}

// SidebarContent returns the interior rect of the (expanded) sidebar box and
// how many rows are visible. Rows scroll inside it; the last interior row is
// the fixed [menu]/[new] footer.
func SidebarContent(area layout.Rect) (x, y, w, rows int) {
	x = area.X + 1
	y = area.Y + 1
	w = area.W - 2
	rows = area.H - 3 // minus top border, footer row, bottom border
	if rows < 0 {
		rows = 0
	}
	return
}

// SidebarFooter returns the rect of the fixed footer row inside the box.
func SidebarFooter(area layout.Rect) layout.Rect {
	return layout.Rect{X: area.X + 1, Y: area.Y + area.H - 2, W: area.W - 2, H: 1}
}

// SidebarToggleRect returns the cell of the « / » collapse-expand button.
func SidebarToggleRect(s *model.AppState) layout.Rect {
	area := s.SidebarArea()
	if s.SidebarVisible {
		return layout.Rect{X: area.X + area.W - 2, Y: area.Y + area.H - 1, W: 1, H: 1}
	}
	return layout.Rect{X: area.X + (area.W-1)/2, Y: area.Y + area.H - 1, W: 1, H: 1}
}

// SidebarMenuRect / SidebarNewRect locate the footer's [menu] and [new].
func SidebarMenuRect(s *model.AppState) layout.Rect {
	fr := SidebarFooter(s.SidebarArea())
	return layout.Rect{X: fr.X + 1, Y: fr.Y, W: 6, H: 1}
}
func SidebarNewRect(s *model.AppState) layout.Rect {
	fr := SidebarFooter(s.SidebarArea())
	return layout.Rect{X: fr.X + fr.W - 6, Y: fr.Y, W: 6, H: 1}
}

func drawSidebar(f *Frame, s *model.AppState, in Input) {
	area := s.SidebarArea()
	if area.W == 0 || area.H == 0 {
		return
	}
	if !s.SidebarVisible {
		drawSidebarCollapsed(f, s, in, area)
		return
	}
	border := uv.Style{Fg: Dim}
	f.Box(area.X, area.Y, area.W, area.H, border)
	ix, iy, iw, avail := SidebarContent(area)
	if iw < 1 {
		return
	}
	f.Text(ix, area.Y, " workspaces ", uv.Style{Fg: Dim}, area.X+area.W-1)

	rows := SidebarRows(s, in.Panes)
	scroll := in.SidebarScroll
	if max := len(rows) - avail; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := scroll; i < len(rows) && i < scroll+avail; i++ {
		drawSidebarRow(f, s, in, rows[i], ix, iy+i-scroll, iw)
	}

	// Footer: [menu] left, [new] right. The copy flash ("Copied N
	// lines") claims the sidebar's bottom text row while shown
	// (SPEC G1).
	fr := SidebarFooter(area)
	if fr.Y >= iy && fr.Y < iy+area.H-2 {
		if in.Copied != "" && fr.W > 1 {
			// SPEC: green bold text on surface_dim, drawn over the
			// sidebar width minus 1 column; the rightmost column is
			// never overwritten, and overflow ends with '…'.
			w := fr.W - 1
			f.Fill(fr.X, fr.Y, w, 1, " ", uv.Style{Bg: SurfDim})
			text := in.Copied
			if len([]rune(text)) > w {
				text = truncateEllipsis(text, w)
			}
			f.Text(fr.X, fr.Y, text, uv.Style{Fg: Green, Bg: SurfDim, Attrs: uv.AttrBold}, fr.X+w)
		} else {
			f.Text(fr.X+1, fr.Y, "[menu]", uv.Style{Fg: Subtle}, fr.X+fr.W)
			f.Text(fr.X+fr.W-6, fr.Y, "[new]", uv.Style{Fg: Subtle}, fr.X+fr.W)
		}
	}
	// Collapse button on the bottom border, second from right.
	f.Set(area.X+area.W-2, area.Y+area.H-1, "«", uv.Style{Fg: Dim})
}

func drawSidebarRow(f *Frame, s *model.AppState, in Input, r SidebarRow, ix, y, iw int) {
	border := uv.Style{Fg: Dim}
	switch r.Kind {
	case RowSep:
		f.Set(ix-1, y, "├", border)
		f.Text(ix, y, "─ agents ", border, ix+iw)
		for xx := ix + 9; xx < ix+iw; xx++ {
			f.Set(xx, y, "─", border)
		}
		f.Set(ix+iw, y, "┤", border)
	case RowWorkspace:
		ws := s.Workspaces[r.Ws]
		g := wsGlyph(s, r.Ws, in)
		active := r.Ws == s.Active
		if active {
			f.Fill(ix, y, iw, 1, " ", uv.Style{Bg: Surface})
		}
		num := fmt.Sprintf(" %d ", r.Ws+1)
		if ws.Worktree != nil {
			if ws.Worktree.IsLinkedWorktree {
				num = fmt.Sprintf("   %d ", r.Ws+1)
			} else {
				arrow := "▾"
				if ws.GroupCollapsed {
					arrow = "▸"
				}
				num = fmt.Sprintf("%s %d ", arrow, r.Ws+1)
			}
		}
		sty := uv.Style{Fg: Text, Attrs: boldIf(active)}
		if active {
			sty.Bg = Surface
		}
		x := f.Text(ix, y, num, sty, ix+iw)
		x = f.Text(x, y, g.Rune, uv.Style{Fg: g.Color, Bg: sty.Bg, Attrs: boldIf(active)}, ix+iw)
		dots := in.JobDots[r.Ws]
		name := " " + truncate(ws.Name, iw-(x-ix)-1-len(dots))
		f.Text(x, y, name, sty, ix+iw)
		// G2: one ● per meaningful background job on this workspace.
		for i, d := range dots {
			f.Set(ix+iw-1-i, y, "●", uv.Style{Fg: d.Color, Bg: sty.Bg})
		}
	case RowAgent:
		snap := in.Panes[r.Pane]
		if snap == nil {
			return
		}
		focused := r.Ws == s.Active
		if l := s.Workspaces[r.Ws].ActiveLayout(); l != nil {
			focused = focused && l.Focused() == r.Pane
		}
		sty := uv.Style{Fg: Text, Attrs: boldIf(focused)}
		x := f.Text(ix+2, y, snap.Status.Rune, uv.Style{Fg: snap.Status.Color}, ix+iw)
		x = f.Text(x, y, fmt.Sprintf(" %d ", r.Pane), sty, ix+iw)
		f.Text(x, y, truncate(snap.Agent, iw-(x-ix)), sty, ix+iw)
	}
}

// truncate cuts s to at most w display cells, ending with "…" when cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	var b strings.Builder
	cur := 0
	for _, r := range s {
		rw := runeW(r)
		if cur+rw+1 > w {
			break
		}
		b.WriteRune(r)
		cur += rw
	}
	return b.String() + "…"
}

func runeW(r rune) int {
	return ansi.StringWidth(string(r))
}

// wsGlyph aggregates the pane glyphs of a workspace into one attention glyph.
func wsGlyph(s *model.AppState, wi int, in Input) StatusGlyph {
	return aggregateGlyph(s.Workspaces[wi], in)
}

func drawSidebarCollapsed(f *Frame, s *model.AppState, in Input, area layout.Rect) {
	border := uv.Style{Fg: Dim}
	ix, iy, iw, ih := f.Box(area.X, area.Y, area.W, area.H, border)
	if iw < 1 || ih < 1 {
		return
	}
	avail := ih
	scroll := in.SidebarScroll
	if max := len(s.Workspaces) - avail; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := scroll; i < len(s.Workspaces) && i < scroll+avail; i++ {
		y := iy + i - scroll
		sty := uv.Style{Fg: Text}
		if i == s.Active {
			sty = uv.Style{Fg: Accent, Attrs: uv.AttrBold}
		}
		x := f.Text(ix, y, fmt.Sprintf("%d", i+1), sty, ix+iw)
		// Aggregate agent glyph for the workspace (highest priority state).
		g := aggregateGlyph(s.Workspaces[i], in)
		f.Text(x, y, g.Rune, uv.Style{Fg: g.Color, Attrs: boldIf(g.Bold)}, ix+iw)
	}
	// Expand button centred on the bottom border row.
	f.Set(area.X+(area.W-1)/2, area.Y+area.H-1, "»", uv.Style{Fg: Dim})
}

// aggregateGlyph picks the most attention-worthy pane glyph of a workspace.
func aggregateGlyph(ws *model.Workspace, in Input) StatusGlyph {
	best := StatusGlyph{Rune: " ", Color: Dim}
	rank := 0
	for _, t := range ws.Tabs {
		for _, pid := range t.Layout.PaneIDs() {
			snap := in.Panes[pid]
			if snap == nil {
				continue
			}
			r := glyphRank(snap.Status)
			if r > rank {
				rank = r
				best = snap.Status
			}
		}
	}
	return best
}

// workingRune returns the spinner frame for the working aggregate glyph
// (SPEC G6: working indicators spin instead of a fixed dot).
func workingRune(spin int) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	if spin < 0 {
		spin = 0
	}
	return frames[spin%len(frames)]
}

// glyphRank orders state keys by attention: blocked > done > working > idle
// > unknown.
func glyphRank(g StatusGlyph) int {
	switch g.Key {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
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

// --- menu ---

// MenuSize computes the box size for a menu.
func MenuSize(m *Menu) (w, h int) {
	w = 0
	for _, it := range m.Items {
		l := len([]rune(it.Label))
		if it.Separator {
			l = 2
		}
		if l > w {
			w = l
		}
	}
	w += 4 // padding + border
	return w, len(m.Items) + 2
}

// MenuRect positions a menu inside (cols, rows).
func MenuRect(m *Menu, cols, rows int) layout.Rect {
	w, h := MenuSize(m)
	x, y := m.X, m.Y
	if x+w > cols {
		x = cols - w
	}
	if y+h > rows {
		y = rows - h
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return layout.Rect{X: x, Y: y, W: w, H: h}
}

func drawMenu(f *Frame, m *Menu, out *Output) {
	r := MenuRect(m, f.W, f.H)
	border := uv.Style{Fg: Dim}
	ix, iy, iw, ih := f.Box(r.X, r.Y, r.W, r.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: Text})
	out.MenuRect = r
	out.MenuItemRows = nil
	for i, it := range m.Items {
		if i >= ih {
			break
		}
		row := layout.Rect{X: ix, Y: iy + i, W: iw, H: 1}
		out.MenuItemRows = append(out.MenuItemRows, row)
		if it.Separator {
			for xx := ix; xx < ix+iw; xx++ {
				f.Set(xx, iy+i, "─", border)
			}
			continue
		}
		sty := uv.Style{Fg: Text}
		if i == m.Hover || i == m.Sel {
			sty = uv.Style{Fg: Text, Bg: Surface}
			f.Fill(ix, iy+i, iw, 1, " ", sty)
		}
		if it.Disabled {
			sty = uv.Style{Fg: Dim}
		}
		f.Text(ix+1, iy+i, it.Label, sty, ix+iw-1)
	}
}

// --- toast ---

// drawToast renders the notification box at the top-right: a 4-row-high
// bordered box with the status dot + title and one body line.
func drawToast(f *Frame, t *Toast, out *Output) {
	w := len([]rune(t.Title)) + 6
	if l := len([]rune(t.Body)) + 4; l > w {
		w = l
	}
	maxW := f.W / 2
	if maxW < 20 {
		maxW = 20
	}
	if w > maxW {
		w = maxW
	}
	x := f.W - w - 1
	if x < 0 {
		x = 0
	}
	y := 1
	r := layout.Rect{X: x, Y: y, W: w, H: 4}
	out.ToastRect = r
	dot := uv.Style{Fg: Accent}
	if t.Kind == "needs_attention" {
		dot = uv.Style{Fg: Yellow}
	}
	ix, iy, iw, _ := f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: Dim})
	f.Fill(ix, iy, iw, 2, " ", uv.Style{Fg: Text})
	f.Set(ix, iy, "●", dot)
	f.Text(ix+2, iy, t.Title, uv.Style{Fg: Text, Attrs: uv.AttrBold}, ix+iw)
	f.Text(ix+1, iy+1, t.Body, uv.Style{Fg: Dim}, ix+iw)
}

// --- dialog ---

// DialogRects returns the outer rect and the cancel/confirm button rects.
func DialogRects(d *Dialog, cols, rows int) (outer, cancel, confirm layout.Rect) {
	w := len([]rune(d.Title)) + 4
	if l := len([]rune(d.Detail)) + 4; l > w {
		w = l
	}
	for _, ln := range d.Lines {
		if l := len([]rune(ln)) + 4; l > w {
			w = l
		}
	}
	if d.InputEdit {
		if l := len([]rune(d.Input)) + 8; l > w {
			w = l
		}
	}
	// "[ <cancel>  esc ]" + gap + "[ <confirm>  ↵ ]" = cancel+9 + 2 + confirm+7.
	btnRow := len([]rune(d.Cancel)) + len([]rune(d.Confirm)) + 18
	if btnRow > w {
		w = btnRow
	}
	if d.Width > w {
		w = d.Width
	}
	shown := len(d.Lines)
	if shown > 10 {
		shown = 10
	}
	h := 5 + shown
	x := (cols - w) / 2
	y := (rows - h) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	outer = layout.Rect{X: x, Y: y, W: w, H: h}
	by := y + h - 2
	// Buttons right-aligned: "[ <confirm>  ↵ ]" then "[ <cancel>  esc ]".
	confirmW := len([]rune(d.Confirm)) + 7
	cancelW := len([]rune(d.Cancel)) + 9
	confirm = layout.Rect{X: x + w - 1 - confirmW, Y: by, W: confirmW, H: 1}
	cancel = layout.Rect{X: confirm.X - 2 - cancelW, Y: by, W: cancelW, H: 1}
	return
}

// drawHelp renders the keybind-help overlay: a centered bordered box
// with a title, subtitle, scrollable binding rows, and a footer hint
// (SPEC G6).
func drawHelp(f *Frame, h *HelpInput, out *Output) {
	w := f.W * 3 / 4
	if w > 68 {
		w = 68
	}
	if w < 34 {
		w = f.W
	}
	if w > f.W {
		w = f.W
	}
	content := len(h.Rows) + 4 // subtitle + rows + footer inside a border
	hgt := content
	if max := f.H - 2; hgt > max {
		hgt = max
	}
	if hgt < 6 {
		hgt = 6
	}
	x := (f.W - w) / 2
	y := (f.H - hgt) / 2
	border := uv.Style{Fg: Dim}
	ix, iy, iw, ih := f.Box(x, y, w, hgt, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: Text})
	out.HelpRect = layout.Rect{X: x, Y: y, W: w, H: hgt}

	// Title on the top border row, subtitle on the first inner row.
	title := " keybinds · scroll to explore · esc close "
	for i, r := range []rune(title) {
		if x+2+i >= x+w-1 {
			break
		}
		f.Set(x+2+i, y, string(r), uv.Style{Fg: Accent})
	}
	f.Text(ix+1, iy, "available commands and configured shortcuts", uv.Style{Fg: Dim}, ix+iw-1)

	// Scrollable content rows.
	body := ih - 2 // subtitle + footer
	if body < 1 {
		body = 1
	}
	scroll := h.Scroll
	if scroll < 0 {
		scroll = 0
	}
	if max := len(h.Rows) - body; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	for i := 0; i < body && scroll+i < len(h.Rows); i++ {
		row := h.Rows[scroll+i]
		ry := iy + 1 + i
		if row.Group {
			f.Text(ix+1, ry, row.Cmd, uv.Style{Fg: Accent, Attrs: uv.AttrBold}, ix+iw-1)
			continue
		}
		f.Text(ix+2, ry, row.Bind, uv.Style{Fg: Peach}, ix+iw-1)
		f.Text(ix+20, ry, row.Cmd, uv.Style{Fg: Text}, ix+iw-1)
	}

	// Footer hint on the last inner row.
	f.Text(ix+1, iy+ih-1, "scroll wheel ↑↓ · jump pgup/pgdn · close esc/enter", uv.Style{Fg: Dim}, ix+iw-1)
}

func drawDialog(f *Frame, d *Dialog, out *Output) {
	outer, cancel, confirm := DialogRects(d, f.W, f.H)
	border := uv.Style{Fg: Red}
	ix, iy, iw, ih := f.Box(outer.X, outer.Y, outer.W, outer.H, border)
	f.Fill(ix, iy, iw, ih, " ", uv.Style{Fg: Text})
	f.Text(ix+1, iy, d.Title, uv.Style{Fg: Text, Attrs: uv.AttrBold}, ix+iw)
	if d.InputEdit {
		f.Text(ix+1, iy+1, "> "+d.Input+" ", uv.Style{Fg: Text, Bg: Surface}, ix+iw)
	} else {
		f.Text(ix+1, iy+1, d.Detail, uv.Style{Fg: Subtle}, ix+iw)
		shown := len(d.Lines)
		if shown > 10 {
			shown = 10
		}
		for i := 0; i < shown; i++ {
			f.Text(ix+1, iy+2+i, d.Lines[i], uv.Style{Fg: Subtle}, ix+iw)
		}
	}
	f.Text(cancel.X, cancel.Y, "[ "+d.Cancel+"  esc ]", uv.Style{Fg: Subtle}, cancel.X+cancel.W)
	f.Fill(confirm.X, confirm.Y, confirm.W, 1, " ", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: Red, Attrs: uv.AttrBold})
	f.Text(confirm.X, confirm.Y, "[ "+d.Confirm+"  ↵ ]", uv.Style{Fg: color.RGBA{0xff, 0xff, 0xff, 0xff}, Bg: Red, Attrs: uv.AttrBold}, confirm.X+confirm.W)
	out.DialogRect = outer
	out.DialogCancel = cancel
	out.DialogConfirm = confirm
}

// --- helpers ---

func label(snap *PaneSnap) string {
	if snap == nil || snap.Label == "" {
		return "—"
	}
	return snap.Label
}

func boldIf(b bool) uint8 {
	if b {
		return uv.AttrBold
	}
	return 0
}
