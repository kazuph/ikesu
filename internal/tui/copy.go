package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/kazuph/goterrace/internal/copybuf"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

// Copy mode (SPEC G6): a tmux-style read view over a pane's scrollback +
// screen buffer, entered via prefix+[ / prefix+] or the action bar's
// COPY button. The button cycles normal → in-layout copy → fullscreen
// copy → normal (" COPY " / " PHONE COPY " / " EXIT COPY ").

type copyPoint struct{ x, y int }

// Page captures at 45, 25 and 35 rows move 39, 19 and 29 lines respectively.
const copyPageOverlapRows = 2

type copyMode struct {
	pane          int
	fullscreen    bool
	cx, cy        int // cursor in buffer coords (scrollback lines first)
	top           int // buffer index of the first visible row
	anchor        *copyPoint
	linewise      bool
	searching     bool
	searchDir     int // +1 after '/', -1 after '?'; drives n/N
	query         string
	previousQuery string
	matches       []copybuf.Range
	matchIdx      int
}

// copyCycle advances the button's 3-state cycle: normal → in-layout
// copy → fullscreen copy → normal (restores the layout on exit).
func (a *App) copyCycle() {
	switch {
	case a.cm == nil:
		a.enterCopy(false)
	case !a.cm.fullscreen:
		a.cm.fullscreen = true
		a.copyEnterFullscreen()
	default:
		a.exitCopy()
	}
}

// enterCopy opens copy mode on the focused pane.
func (a *App) enterCopy(fullscreen bool) {
	pid := a.focusedPane()
	p := a.sup.Get(pid)
	if p == nil {
		return
	}
	n := a.bufferLen(p)
	a.sel = nil
	snapshot := p.Snapshot()
	a.cm = &copyMode{pane: pid, cx: snapshot.Cursor.X, cy: min(snapshot.ScrollbackLen+snapshot.Cursor.Y, max(n-1, 0)), searchDir: 1}
	a.cm.top = max(n-a.copyInnerH()-a.scroll[pid], 0)
	if a.cm.cy < a.cm.top || a.cm.cy >= a.cm.top+a.copyInnerH() {
		a.cm.cx, a.cm.cy = 0, a.cm.top+a.copyInnerH()-1
	}
	if fullscreen {
		a.cm.fullscreen = true
		a.copyEnterFullscreen()
	}
	a.copyClampCursor()
	a.copyReveal()
	a.dirty = true
}

// Fullscreen enlarges the copy view while keeping the shell's PTY size.
func (a *App) copyEnterFullscreen() {
	previousTop := a.cm.top
	a.cm.top = min(a.cm.top, max(a.bufferLen(a.sup.Get(a.cm.pane))-a.copyInnerH(), 0))
	a.cm.cy += a.cm.top - previousTop
	a.copyClampCursor()
	a.copyReveal()
}

// exitCopy leaves copy mode and restores the pane's layout size.
func (a *App) exitCopy() {
	a.cm = nil
	a.syncLayout()
	a.dirty = true
}

// copyInnerH is the viewport height of the copy view.
func (a *App) copyInnerH() int {
	if a.cm.fullscreen {
		return max(a.st.PanesArea().H, 1)
	}
	return a.innerRect(a.cm.pane).H
}

// copyInnerW mirrors the terminal width inside the copy view.
func (a *App) copyInnerW() int {
	if a.cm.fullscreen {
		return max(a.st.PanesArea().W, 1)
	}
	return a.innerRect(a.cm.pane).W
}

func (a *App) copyClampCursor() {
	p := a.sup.Get(a.cm.pane)
	if p == nil {
		return
	}
	n := a.bufferLen(p)
	if n == 0 {
		n = 1
	}
	a.cm.cy = min(max(a.cm.cy, 0), n-1)
	a.cm.cx = min(max(a.cm.cx, 0), max(a.copyInnerW()-1, 0))
}

// copyReveal scrolls the view so the cursor row is inside the viewport.
func (a *App) copyReveal() {
	p := a.sup.Get(a.cm.pane)
	if p == nil {
		return
	}
	h := a.copyInnerH()
	a.cm.top = max(a.cm.top, 0)
	if a.cm.cy < a.cm.top {
		a.cm.top = a.cm.cy
	}
	if a.cm.cy >= a.cm.top+h {
		a.cm.top = a.cm.cy - h + 1
	}
	if n := a.bufferLen(p); a.cm.top > n-h {
		a.cm.top = max(n-h, 0)
	}
}

// copyKey handles one key inside copy mode (SPEC G6 本家 copy mode):
// arrows/hjkl, w/b/e word motion, 0/$ real line ends, g/G, PgUp/PgDn,
// C-u/C-d half page, / search, n/N matches, Space select, Enter yank,
// q/Esc exit; prefix stays available (C-b).
func (a *App) copyKey(k uv.Key, raw []byte) {
	cm := a.cm
	p := a.sup.Get(cm.pane)
	if p == nil {
		a.cm = nil
		return
	}
	if cm.searching {
		a.copySearchKey(k)
		return
	}
	if a.prefixMatches(k) {
		a.prefix = true
		return
	}
	if k.Code >= 'a' && k.Code <= 'z' && k.Mod.Contains(uv.ModShift) {
		k.Code -= 'a' - 'A'
	}
	n := a.bufferLen(p)
	h := a.copyInnerH()
	switch {
	case k.Code == uv.KeyEscape && (cm.query != "" || cm.anchor != nil):
		cm.query, cm.matches, cm.matchIdx = "", nil, -1
		cm.anchor = nil
		a.dirty = true
		return
	case k.Code == 'q' || k.Code == uv.KeyEscape:
		a.exitCopy()
		return
	case k.Code == uv.KeyLeft || k.Code == 'h':
		cm.cx--
	case k.Code == uv.KeyRight || k.Code == 'l':
		cm.cx++
	case k.Code == uv.KeyUp || k.Code == 'k':
		cm.cy--
	case k.Code == uv.KeyDown || k.Code == 'j':
		cm.cy++
	case k.Code == 'w':
		a.copyMotion(copybuf.MotionNextWordStart)
	case k.Code == 'b' && !k.Mod.Contains(uv.ModCtrl):
		a.copyMotion(copybuf.MotionPreviousWordStart)
	case k.Code == 'e':
		a.copyMotion(copybuf.MotionNextWordEnd)
	case k.Code == '{':
		a.copyMotion(copybuf.MotionPreviousParagraph)
	case k.Code == '}':
		a.copyMotion(copybuf.MotionNextParagraph)
	case k.Code == '0' || k.Code == uv.KeyHome:
		cm.cx = 0
	case k.Code == '^':
		a.copyMotion(copybuf.MotionFirstNonBlank)
	case k.Code == '$' || k.Code == uv.KeyEnd:
		a.copyMotion(copybuf.MotionLineEnd)
	case k.Code == 'g':
		cm.cy = 0
	case k.Code == 'G':
		cm.cy = n - 1
	case k.Code == uv.KeyPgUp || k.Code == 'b' && k.Mod.Contains(uv.ModCtrl):
		step := max(h-copyPageOverlapRows, 1)
		cm.cy -= step
		cm.top -= step
	case k.Code == uv.KeyPgDown || k.Code == 'f' && k.Mod.Contains(uv.ModCtrl):
		step := max(h-copyPageOverlapRows, 1)
		cm.cy += step
		cm.top += step
	case k.Code == 'u' && k.Mod.Contains(uv.ModCtrl):
		cm.cy -= max(h/2, 1)
		cm.top -= max(h/2, 1)
	case k.Code == 'd' && k.Mod.Contains(uv.ModCtrl):
		cm.cy += max(h/2, 1)
		cm.top += max(h/2, 1)
	case k.Code == '/':
		cm.searching = true
		cm.previousQuery = cm.query
		cm.searchDir = 1
		cm.query = ""
	case k.Code == '?':
		cm.searching = true
		cm.previousQuery = cm.query
		cm.searchDir = -1
		cm.query = ""
	case k.Code == 'n':
		a.copyNextMatch(cm.searchDir)
	case k.Code == 'N':
		a.copyNextMatch(-cm.searchDir)
	case k.Code == 'V':
		cm.linewise = true
		cm.anchor = &copyPoint{x: cm.cx, y: cm.cy}
	case k.Code == ' ' || k.Code == 'v':
		cm.linewise = false
		cm.anchor = &copyPoint{x: cm.cx, y: cm.cy}
	case k.Code == uv.KeyEnter || k.Code == 'y':
		if cm.anchor != nil {
			a.copyYank()
		}
		a.exitCopy()
		return
	default:
		return
	}
	a.copyClampCursor()
	a.copyReveal()
	a.dirty = true
}

// copyMotion applies a copybuf motion (shared with pane.copy_motion) to
// the copy cursor.
func (a *App) copyMotion(motion string) {
	cm := a.cm
	p := a.sup.Get(cm.pane)
	if p == nil {
		return
	}
	to, ok := copybuf.Motion(p.BufferLines(), motion, cm.cy, cm.cx)
	if ok {
		cm.cx, cm.cy = to.Col, to.Row
	}
}

// copySearchKey feeds the in-flight search query.
func (a *App) copySearchKey(k uv.Key) {
	cm := a.cm
	switch {
	case k.Code == uv.KeyEnter:
		cm.searching = false
		if cm.query != "" {
			a.copyFindAll()
			a.copyNextMatch(cm.searchDir)
		}
	case k.Code == uv.KeyEscape:
		cm.searching = false
		cm.query = cm.previousQuery
	case k.Code == 'u' && k.Mod.Contains(uv.ModCtrl):
		cm.query = ""
	case k.Code == uv.KeyBackspace || k.Code == uv.KeyDelete:
		if r := []rune(cm.query); len(r) > 0 {
			cm.query = string(r[:len(r)-1])
		}
	case k.Text != "":
		cm.query += k.Text
	}
	a.dirty = true
}

// copyFindAll applies smart case to the UI search only. The API keeps
// its case-sensitive copybuf.Search contract.
func (a *App) copyFindAll() {
	cm := a.cm
	p := a.sup.Get(cm.pane)
	if p == nil {
		return
	}
	lines, query := p.BufferLines(), cm.query
	if strings.IndexFunc(query, unicode.IsUpper) < 0 {
		query = strings.ToLower(query)
		for y, row := range lines {
			lines[y] = append([]uv.Cell(nil), row...)
			for x := range lines[y] {
				lines[y][x].Content = strings.ToLower(lines[y][x].Content)
			}
		}
	}
	cm.matches = copybuf.Search(lines, query)
	cm.matchIdx = -1
}

// copyNextMatch jumps to the match strictly after/before the cursor
// (wrapping) — the same rule as pane.copy_search's current index.
func (a *App) copyNextMatch(dir int) {
	cm := a.cm
	if cm.query == "" {
		return
	}
	if len(cm.matches) == 0 {
		a.copyFindAll()
	}
	if len(cm.matches) == 0 {
		return
	}
	anchor := copybuf.Point{Row: cm.cy, Col: cm.cx}
	if cm.matchIdx >= 0 {
		// Repeat n/N steps relative to the last landed match.
		anchor = cm.matches[cm.matchIdx].Start
	}
	idx := copybuf.CurrentIndex(cm.matches, anchor, dir > 0)
	cm.matchIdx = idx
	m := cm.matches[idx]
	cm.cx, cm.cy = m.Start.Col, m.Start.Row
	a.copyClampCursor()
	a.copyReveal()
}

// copySelRange returns the normalized (from,to) of the current selection.
func (a *App) copySelRange() (copyPoint, copyPoint, bool) {
	cm := a.cm
	if cm.anchor == nil {
		return copyPoint{}, copyPoint{}, false
	}
	from, to := *cm.anchor, copyPoint{x: cm.cx, y: cm.cy}
	if to.y < from.y || (to.y == from.y && to.x < from.x) {
		from, to = to, from
	}
	if cm.linewise {
		from.x, to.x = 0, a.copyInnerW()-1
	}
	return from, to, true
}

// copyYank copies the selection via OSC 52 and flashes "Copied N lines"
// on the sidebar's bottom row for 2 s (SPEC G1/G6).
func (a *App) copyYank() {
	cm := a.cm
	from, to, ok := a.copySelRange()
	if !ok {
		return
	}
	p := a.sup.Get(cm.pane)
	if p == nil {
		return
	}
	text := a.copyText(p, from, to)
	if text == "" {
		return
	}
	if err := a.writeClipboard(text); err != nil {
		return
	}
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	a.copied = fmt.Sprintf("Copied %d lines", lines)
	a.copiedAt = time.Now()
}

// copyText extracts the buffer text between two points (inclusive),
// trailing blanks trimmed per line.
func (a *App) copyText(p *runtime.Pane, from, to copyPoint) string {
	var b strings.Builder
	for y := from.y; y <= to.y; y++ {
		row := a.bufferLine(p, y)
		x0 := 0
		if y == from.y {
			x0 = from.x
		}
		x1 := len(row) - 1
		if y == to.y {
			x1 = to.x
		}
		var line strings.Builder
		for x := x0; x <= x1 && x < len(row); x++ {
			line.WriteString(row[x].Content)
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		if y < to.y {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// copyLabel returns the action bar's copy button text (fullscreen takes
// precedence over in-layout copy, per SPEC G1).
func (a *App) copyLabel() string {
	if a.cm == nil {
		return " COPY "
	}
	if a.cm.fullscreen {
		return " EXIT COPY "
	}
	return " PHONE COPY "
}

// copyHint builds the copy-mode help row text shown on the pane's
// bottom frame row (SPEC G6; observed upstream row text).
func (a *App) copyHint() string {
	cm := a.cm
	if cm == nil {
		return ""
	}
	if cm.searching {
		p := "/"
		if cm.searchDir < 0 {
			p = "?"
		}
		return " COPY  " + p + cm.query + "█  enter search  esc cancel"
	}
	hint := " COPY  h/j/k/l w/b/e { } move  / ? search  n/N repeat"
	selection := "select"
	if cm.anchor != nil {
		selection = "selecting"
	}
	if cm.query != "" || len(cm.matches) > 0 {
		hint += fmt.Sprintf(" %d/%d", cm.matchIdx+1, len(cm.matches))
	}
	hint += "  v/space " + selection + "  y/enter copy"
	if cm.query != "" || cm.anchor != nil {
		return hint + "  esc clear  q exit"
	}
	return hint + "  q/esc exit"
}

// copyMark yields render's cursor + fullscreen data for the copy view.
func (a *App) copyMark() *render.CopyMark {
	cm := a.cm
	if cm == nil {
		return nil
	}
	m := &render.CopyMark{Pane: cm.pane, X: cm.cx, Y: cm.cy - cm.top, Fullscreen: cm.fullscreen}
	if from, to, ok := a.copySelRange(); ok {
		m.Sel = &render.Selection{
			Pane:  cm.pane,
			FromX: from.x, FromY: from.y - cm.top,
			ToX: to.x, ToY: to.y - cm.top,
		}
	}
	return m
}

// copyGrid builds the copy view's visible rows from the buffer.
func (a *App) copyGrid() [][]uv.Cell {
	cm := a.cm
	p := a.sup.Get(cm.pane)
	if p == nil {
		return nil
	}
	h, w := a.copyInnerH(), a.copyInnerW()
	palette := render.ConfigPalette(a.sidebarConfig)
	if a.settings != nil {
		palette = render.CustomPalette(a.settings.Theme, a.sidebarConfig.Theme.Custom)
	}
	g := make([][]uv.Cell, 0, h)
	for r := 0; r < h; r++ {
		row := a.bufferLine(p, cm.top+r)
		if len(row) > w {
			row = row[:w]
		}
		g = append(g, row)
	}
	for i, match := range cm.matches {
		row := match.Start.Row - cm.top
		if row < 0 || row >= len(g) {
			continue
		}
		style := uv.Style{Fg: palette.Text, Bg: palette.NavigatorDivider}
		if i == cm.matchIdx {
			style = uv.Style{Fg: palette.SurfDim, Bg: palette.Accent, Attrs: uv.AttrBold}
		}
		for col := match.Start.Col; col <= match.End.Col && col < len(g[row]); col++ {
			g[row][col].Style = style
		}
	}
	return g
}

// copyMouse routes mouse events while copy mode is active: wheel scrolls
// the view, left-drag selects, release yanks + exits (tmux behavior).
func (a *App) copyMouse(ev input.Event) {
	m := ev.Mouse
	cm := a.cm
	if cm == nil || a.sup.Get(cm.pane) == nil {
		return
	}
	inner := a.copyViewRect()
	switch ev.Kind {
	case input.KindMouseWheel:
		if a.serverUI {
			return
		}
		previousTop := cm.top
		if m.Button == uv.MouseWheelUp {
			cm.top -= 3
		} else if m.Button == uv.MouseWheelDown {
			cm.top += 3
		}
		n := a.bufferLen(a.sup.Get(cm.pane))
		cm.top = min(max(cm.top, 0), max(n-a.copyInnerH(), 0))
		cm.cy += cm.top - previousTop
	case input.KindMouseClick:
		if m.Button == uv.MouseLeft && pointIn(m.X, m.Y, inner) {
			a.cm.anchor = &copyPoint{x: m.X - inner.X, y: cm.top + m.Y - inner.Y}
			cm.cx, cm.cy = cm.anchor.x, cm.anchor.y
		}
	case input.KindMouseMotion:
		if cm.anchor != nil {
			cm.cx = min(max(m.X-inner.X, 0), a.copyInnerW()-1)
			cm.cy = min(max(cm.top+m.Y-inner.Y, 0), a.bufferLen(a.sup.Get(cm.pane))-1)
		}
	case input.KindMouseRelease:
		if cm.anchor != nil {
			a.copyClampCursor()
			a.copyYank()
			a.exitCopy()
			return
		}
	}
	a.dirty = true
}

// copyViewRect is the screen rect the copy view occupies.
func (a *App) copyViewRect() layout.Rect {
	if a.cm.fullscreen {
		return a.st.PanesArea()
	}
	return a.innerRect(a.cm.pane)
}

// copyFocusChanged keeps a pane's read position while another pane is active.
func (a *App) copyFocusChanged() {
	ws := a.st.ActiveWorkspace()
	for pid := range a.paneCopies {
		if ws == nil || ws.ActiveLayout() == nil || !ws.ActiveLayout().HasPane(pid) {
			delete(a.paneCopies, pid)
		}
	}
	if a.cm != nil && a.cm.pane != a.focusedPane() {
		if a.paneCopies == nil {
			a.paneCopies = map[int]*copyMode{}
		}
		// Observed 0.2.57 preserves search and cursor, but clears the selection.
		a.cm.anchor = nil
		a.paneCopies[a.cm.pane] = a.cm
		a.cm = nil
	}
	if a.cm == nil {
		pid := a.focusedPane()
		a.cm = a.paneCopies[pid]
		delete(a.paneCopies, pid)
	}
}
