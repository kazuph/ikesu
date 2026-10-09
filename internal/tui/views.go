package tui

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"time"
)

// focusedPane returns the focused pane id of the active workspace.
func (a *App) focusedPane() int {
	if a.st.PopupVisible() {
		return a.st.Popup.PaneID
	}
	return a.st.FocusedPane()
}

// glyph returns the pane's display status glyph.
func (a *App) glyph(pid int) render.StatusGlyph {
	tr := a.trackers[pid]
	key := "unknown"
	if tr != nil {
		key = tr.Key()
	}
	if r, ok := a.reported[pid]; ok && r.state != "" {
		key = r.state
	}
	switch key {
	case "working":
		return render.StatusGlyph{Key: "working", Rune: spinnerFrames[a.spin%len(spinnerFrames)], Color: render.Yellow}
	case "blocked":
		return render.StatusGlyph{Key: "blocked", Rune: "▲", Color: render.Peach}
	case "done":
		return render.StatusGlyph{Key: "done", Rune: "●", Color: render.Green}
	case "idle":
		return render.StatusGlyph{Key: "idle", Rune: "○", Color: render.Dim}
	}
	return render.StatusGlyph{Key: "unknown", Rune: "·", Color: render.Dim}
}

// draw composes and writes one frame.
func (a *App) draw() {
	if a.sinkOn != nil && !a.sinkOn() {
		// No display attached: drop the frame work but clear the flag so a
		// later attach gets a full repaint via the resize/reset path.
		a.dirty = false
		a.lastDraw = time.Now()
		return
	}
	panes := a.paneSnaps()
	if a.st.PopupVisible() {
		if p := a.sup.Get(a.st.Popup.PaneID); p != nil {
			r := a.innerRect(p.ID)
			sn := p.Snapshot()
			panes[p.ID] = &render.PaneSnap{Grid: a.paneGrid(p, a.scroll[p.ID], r.H, r.W), Label: "terminal", Title: sn.OSCTitle, Cursor: render.CursorInfo{X: sn.Cursor.X, Y: sn.Cursor.Y, Visible: sn.Cursor.Visible}, ScrollOff: a.scroll[p.ID], ScrollN: sn.ScrollbackLen}
		}
	}
	tokens := map[int]map[string]string{}
	if a.workspaceTokenSource != nil {
		for i, w := range a.st.Workspaces {
			tokens[i] = a.workspaceTokenSource(w.ID)
		}
	}
	if a.cm != nil {
		if snap := panes[a.cm.pane]; snap != nil {
			snap.Grid = a.copyGrid()
		}
	}
	in := render.Input{
		State:          a.st,
		Navigator:      a.navigatorRender(),
		ConfigWarning:  a.configWarning,
		Settings:       a.settings,
		MobileSwitcher: a.mobileSwitcher, MobileScroll: a.mobileScroll,
		SidebarConfig: a.sidebarConfig, WorkspaceTokens: tokens,
		Panes:         panes,
		Scroll:        a.scroll,
		Spin:          a.spin,
		Copied:        a.copiedFlash(),
		Prefix:        a.prefix,
		ModeHint:      a.modeHint(),
		SidebarScroll: a.sidebarScroll,
		AgentScroll:   a.agentScroll,
		JobDots:       a.jobDots,
		Jobs:          a.jobRows, JobsPanel: a.jobsPanel, JobScroll: a.jobScroll,
		Git: a.workspaceGit, WorkspaceNames: a.workspaceNames,
		CopyLabel: a.copyLabel(),
		Copy:      a.copyMark(),
		CopyHint:  a.copyHint(),
	}
	if a.workspaceNavigate {
		in.SelectedWorkspace = &a.navigateSelected
	}
	if a.sidebarGesture != nil {
		in.Dragging = a.sidebarGesture.moved
		in.DraggedWorkspace = a.sidebarGesture.ws
		in.DragSection = a.sidebarGesture.section
		if a.sidebarGesture.moved && a.sidebarGesture.drop >= 0 {
			in.DropIndex = &a.sidebarGesture.drop
		}
	}
	if a.menu != nil {
		in.Menu = &a.menu.m
		if a.menu.kind == menuWorkspace {
			in.SelectedWorkspace = &a.menu.ws
		}
	}
	if a.dialog != nil {
		in.Dialog = &a.dialog.d
	}
	if a.sel != nil && a.sel.moved {
		in.Sel = a.selRender()
	}
	if a.toast != nil {
		in.Toast = &render.Toast{Title: a.toast.title, Body: a.toast.body, Kind: a.toast.kind}
	}
	if a.help != nil {
		in.Help = &render.HelpInput{Rows: a.helpRows(), Scroll: a.help.scroll}
	}
	out := render.Compose(in)
	a.tabHits = out.TabHits
	a.newTabRect = out.NewTabRect
	a.toastRect = out.ToastRect
	a.actionBar = out.ActionBar
	if a.help != nil {
		a.help.rect = out.HelpRect
	}
	a.lastDraw = time.Now()
	a.dirty = false
	if _, err := a.out.Write(a.enc.Frame(out.Frame, out.AnchorX, out.AnchorY, out.AnchorVisible)); err != nil {
		a.enc.Reset()
		a.dirty = true
	}
}

// ResetEncoder drops the diff state so the next frame repaints fully. The
// server calls it when a fresh client attaches.
func (a *App) ResetEncoder() { a.enc.Reset() }

// ScrollOffset returns the client's scrollback offset for a pane.
// Call on the app loop.
func (a *App) ScrollOffset(pid int) int { return a.scroll[pid] }

// PaneStatusKey returns the pane's agent status key
// (working|blocked|done|idle|unknown). Call on the app loop.
func (a *App) PaneStatusKey(pid int) string { return a.glyph(pid).Key }

// SetReported applies a pane.report_agent override. Call on the app loop.
func (a *App) SetReported(pid int, agent, state string) {
	if a.reported == nil {
		a.reported = map[int]reportedPane{}
	}
	prev := a.reported[pid]
	if agent == "" && state == "" {
		delete(a.reported, pid)
	} else {
		a.reported[pid] = reportedPane{agent: agent, state: state}
	}
	if a.availHook != nil {
		cur := a.reported[pid]
		if prev != cur {
			a.availHook(pid)
		}
	}
	a.changed()
}

// Reported returns the API-reported agent override, if any. Call on the
// app loop.
func (a *App) Reported(pid int) (agent, state string, ok bool) {
	r, ok := a.reported[pid]
	return r.agent, r.state, ok
}

// reportedPane holds pane.report_agent overrides.
type reportedPane struct {
	agent string
	state string
}

// fossilWatch tracks a Claude/Codex pane's live-activity fingerprint: the
// moment it last changed. 15 unchanged seconds turn a working verdict
// into idle (SPEC G3 fossil expiry).
type fossilWatch struct {
	fp    string
	since time.Time
}

// paneSnaps builds render snapshots for every live pane.
func (a *App) paneSnaps() map[int]*render.PaneSnap {
	out := map[int]*render.PaneSnap{}
	a.sessionCwds = make(map[int]string, len(a.st.Workspaces))
	active := map[int]bool{}
	if ws := a.st.ActiveWorkspace(); ws != nil {
		if l := ws.ActiveLayout(); l != nil {
			for _, id := range l.PaneIDs() {
				active[id] = true
			}
		}
	}
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				p := a.sup.Get(pid)
				if p == nil {
					continue
				}
				sn := p.Snapshot()
				if t == ws.TabRef() && pid == t.Layout.Focused() && sn.Cwd != "" {
					a.sessionCwds[ws.ID] = sn.Cwd
				}
				snap := &render.PaneSnap{
					Label:  labelFor(sn, p),
					Title:  sn.OSCTitle,
					Agent:  agentFor(sn),
					Status: a.glyph(pid),
					Dead:   sn.Dead,
					Cursor: render.CursorInfo{X: sn.Cursor.X, Y: sn.Cursor.Y, Visible: sn.Cursor.Visible},
				}
				if rep := a.reported[pid]; rep.agent != "" {
					snap.Label, snap.Agent = rep.agent, rep.agent
				}
				if rec := a.sessions[pid]; rec != nil {
					snap.TaskTitle = rec.Title
				}
				if label := a.st.PaneLabels[model.PaneID(pid)]; label != "" {
					snap.Label = label
				}
				if active[pid] {
					r := a.innerRect(pid)
					snap.Grid = a.paneGrid(p, a.scroll[pid], r.H, r.W)
					snap.ScrollOff = a.scroll[pid]
					snap.ScrollN = sn.ScrollbackLen
				}
				if a.paneMetadataSource != nil {
					m := a.paneMetadataSource(pid)
					snap.Tokens = m.Tokens
					snap.StateLabels = m.StateLabels
					snap.DisplayAgent = m.DisplayAgent
					if m.Title != "" {
						snap.TaskTitle = m.Title
					}
				}
				snap.Branch = gitBranch(sn.Cwd, ws.Cwd)
				snap.Agent, snap.Restore = a.paneRestoreLabel(pid, snap.Agent)
				out[pid] = snap
			}
		}
	}
	return out
}

func labelFor(sn runtime.Snapshot, p *runtime.Pane) string {
	// SPEC G1: the border label is the agent name when one is
	// identified, else the literal "terminal" ("%1 terminal").
	if sn.AgentID != "" {
		return sn.AgentID
	}
	if argv := p.SpawnArgv(); len(argv) > 0 {
		return filepath.Base(argv[0])
	}
	return "terminal"
}

// agentFor reports the detected agent id for a snapshot.
func agentFor(sn runtime.Snapshot) string {
	return sn.AgentID
}

// --- helpers ---

type winsize struct{ Cols, Rows, Xpixel, Ypixel int }

// GetWinsize reads the terminal size of f in cells and pixels. Pixel
// dimensions may be zero on platforms that do not report them.
func GetWinsize(f *os.File) (winsize, error) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return winsize{120, 40, 0, 0}, err
	}
	return winsize{int(ws.Col), int(ws.Row), int(ws.Xpixel), int(ws.Ypixel)}, nil
}

// Winsize is the terminal size exported for the client package.
type Winsize = winsize

func readInput(f *os.File, ch chan<- []byte) {
	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			ch <- b
		}
		if err != nil {
			close(ch)
			return
		}
	}
}

// DrainInput reads and discards leftover input (e.g. mouse reports already
// in flight) for up to d, so the outer shell never sees them.
func DrainInput(f *os.File, d time.Duration) {
	deadline := time.Now().Add(d)
	fd := int(f.Fd())
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	buf := make([]byte, 64*1024)
	for {
		remain := time.Until(deadline)
		if remain <= 0 {
			return
		}
		ms := int(remain / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		n, err := unix.Poll(pfd, ms)
		if err != nil || n == 0 {
			return
		}
		if _, err := f.Read(buf); err != nil {
			return
		}
	}
}

// pointIn reports whether (x,y) is inside r.
func pointIn(x, y int, r layout.Rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// applyFossilExpiry downgrades a Claude/Codex working verdict whose live
// activity fingerprint has not changed for fossilExpiry to idle (SPEC
// G3): scrollback spinner/hint lines stop being evidence. Other agents,
// working verdicts without a fingerprint, and non-working verdicts are
// untouched; a changed fingerprint restarts the window and keeps the
// pane working.
func (a *App) applyFossilExpiry(pid int, agentID, screen string, d detect.Detection, now time.Time) detect.Detection {
	if agentID != "claude" && agentID != "codex" {
		return d
	}
	if d.State != detect.Working {
		delete(a.fossils, pid)
		return d
	}
	fp := detect.ActivityFingerprint(agentID, screen)
	if fp == "" {
		delete(a.fossils, pid)
		return d // fingerprint-free working evidence stays working
	}
	w := a.fossils[pid]
	if w == nil || w.fp != fp {
		if a.fossils == nil {
			a.fossils = map[int]*fossilWatch{}
		}
		a.fossils[pid] = &fossilWatch{fp: fp, since: now}
		return d
	}
	if now.Sub(w.since) < fossilExpiry {
		return d
	}
	// Fossilized: the visible working evidence is stale scrollback.
	d.State = detect.Idle
	d.VisibleWorking = false
	d.Correction = true
	return d
}

// clearStaleReport drops a reported working/blocked authority once screen
// detection for the same Claude/Codex agent has shown explicit visible
// idle for the fossil window (SPEC G3). Reports are non-full-lifecycle
// authorities, so all of them are clearable; the pane's recorded agent
// session is untouched — only the stale state override goes away.
func (a *App) clearStaleReport(pid int, agentID string, d detect.Detection, now time.Time) {
	rep, ok := a.reported[pid]
	if !ok || (rep.state != "working" && rep.state != "blocked") {
		delete(a.visibleIdle, pid)
		return
	}
	if agentID == "" || rep.agent != "" && rep.agent != agentID {
		delete(a.visibleIdle, pid)
		return
	}
	if agentID != "claude" && agentID != "codex" {
		delete(a.visibleIdle, pid)
		return
	}
	if d.State != detect.Idle || !d.VisibleIdle {
		delete(a.visibleIdle, pid)
		return
	}
	if a.visibleIdle == nil {
		a.visibleIdle = map[int]time.Time{}
	}
	if a.visibleIdle[pid].IsZero() {
		a.visibleIdle[pid] = now
		return
	}
	if now.Sub(a.visibleIdle[pid]) < fossilExpiry {
		return
	}
	delete(a.reported, pid)
	delete(a.visibleIdle, pid)
	if a.availHook != nil {
		a.availHook(pid)
	}
	a.dirty = true
}

func (a *App) SetPaneMetadataSource(f func(int) api.PaneMetadataView) { a.paneMetadataSource = f }

func (a *App) SetConfigWarning(message string) {
	a.configWarning = message
	a.dirty = true
}
