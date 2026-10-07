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
	Grid      [][]uv.Cell // content cells, [row][col], scroll applied
	Label     string      // process/agent name for the border title
	Agent     string      // detected agent id ("" = not identified)
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
	// Input/InputEdit render the detail row as an editable text field.
	Input     string
	InputEdit bool
}

// Input is everything Compose needs for one frame.
type Input struct {
	State  *model.AppState
	Panes  map[int]*PaneSnap
	Scroll map[int]int

	Menu   *Menu
	Dialog *Dialog
	Sel    *Selection
	Toast  *Toast

	Spin          int    // working-spinner frame
	Copied        string // flash text, e.g. " copied 12 chars "
	Prefix        bool   // prefix-key pending
	SidebarScroll int    // sidebar rows scrolled off the top
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
	// MenuRect/DialogRects/ToastRect let the mouse layer hit-test overlays.
	ToastRect     layout.Rect
	MenuRect      layout.Rect
	MenuItemRows  []layout.Rect // per-item row rects (absolute coords)
	DialogRect    layout.Rect
	DialogConfirm layout.Rect
	DialogCancel  layout.Rect
}

// Compose renders the entire screen into a frame.
func Compose(in Input) *Output {
	s := in.State
	f := NewFrame(s.Cols, s.Rows)
	out := &Output{Frame: f, AnchorX: s.Cols - 1, AnchorY: s.Rows - 1}

	drawSidebar(f, s, in)
	drawTabBar(f, s, out)
	drawPanes(f, s, in, out)
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
	l := ws.ActiveLayout()
	if l == nil {
		return
	}
	for _, pr := range l.Panes(area) {
		snap := in.Panes[pr.ID]
		borderSty := uv.Style{Fg: Dim}
		if pr.IsFocused {
			borderSty = uv.Style{Fg: Accent}
		}
		if snap != nil && snap.Dead {
			borderSty = uv.Style{Fg: Dim, Attrs: uv.AttrFaint}
		}
		ix, iy, iw, ih := f.Box(pr.Rect.X, pr.Rect.Y, pr.Rect.W, pr.Rect.H, borderSty)

		// Title: " <id> <label> "
		title := fmt.Sprintf(" %d %s ", pr.ID, label(snap))
		f.Text(pr.Rect.X+1, pr.Rect.Y, title, borderSty, pr.Rect.X+pr.Rect.W-1)

		// Scroll indicator on the top-right border.
		if off := in.Scroll[pr.ID]; off > 0 {
			ind := fmt.Sprintf(" ↑%d ", off)
			w := len([]rune(ind))
			f.Text(pr.Rect.X+pr.Rect.W-1-w, pr.Rect.Y, ind, uv.Style{Fg: Yellow})
		}

		// Content.
		if snap != nil {
			f.CopyGrid(ix, iy, clipGrid(snap.Grid, iw, ih))
		}

		// Selection highlight.
		if in.Sel != nil && in.Sel.Pane == pr.ID {
			applySelection(f, ix, iy, iw, ih, in.Sel)
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

	// Footer: [menu] left, [new] right.
	fr := SidebarFooter(area)
	if fr.Y >= iy && fr.Y < iy+area.H-2 {
		f.Text(fr.X+1, fr.Y, "[menu]", uv.Style{Fg: Subtle}, fr.X+fr.W)
		f.Text(fr.X+fr.W-6, fr.Y, "[new]", uv.Style{Fg: Subtle}, fr.X+fr.W)
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
		sty := uv.Style{Fg: Text, Attrs: boldIf(active)}
		if active {
			sty.Bg = Surface
		}
		x := f.Text(ix, y, num, sty, ix+iw)
		x = f.Text(x, y, g.Rune, uv.Style{Fg: g.Color, Bg: sty.Bg, Attrs: boldIf(active)}, ix+iw)
		name := " " + truncate(ws.Name, iw-(x-ix)-1)
		f.Text(x, y, name, sty, ix+iw)
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
		{"working", "◐", Yellow}, {"blocked", "▲", Peach}, {"done", "●", Green}, {"idle", "○", Dim},
	}
	for _, o := range order {
		if n := counts[o.key]; n > 0 {
			txt := fmt.Sprintf("%s %d ", o.g, n)
			right -= len([]rune(txt))
			f.Text(right, y, txt, uv.Style{Fg: o.c, Bg: Surface}, s.Cols-1)
		}
	}

	// Copied flash between the left label and the right cluster.
	if in.Copied != "" {
		f.Text(x+2, y, in.Copied, uv.Style{Fg: Green, Bg: Surface}, right)
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
	h := 5
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
