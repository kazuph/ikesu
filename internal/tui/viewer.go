package tui

import (
	"io"
	"time"

	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
)

// Viewer owns presentation and input state. The model and PTYs remain
// session-owned; all methods run on the session's existing app loop.
type Viewer struct {
	app               *App
	workspace         int
	tabs              map[int]int
	cols, rows        int
	toastSequence     uint64
	pendingAt         time.Time
	focusUntil        time.Time
	focusEpoch        uint64
	heldKeys          []input.Event
	display           displayState
	legacySidebarRows bool
}

func (v *Viewer) SetContinuousSidebarRows(supported bool) {
	v.legacySidebarRows = !supported
}

type viewSelection struct {
	workspace  int
	tabs       map[int]int
	cols, rows int
	display    displayState
}

func (a *App) selection() viewSelection {
	s := viewSelection{tabs: map[int]int{}, cols: a.st.Cols, rows: a.st.Rows, display: captureDisplay(a.st)}
	if ws := a.st.ActiveWorkspace(); ws != nil {
		s.workspace = ws.ID
	}
	for _, ws := range a.st.Workspaces {
		if t := ws.TabRef(); t != nil {
			s.tabs[ws.ID] = t.ID
		}
	}
	return s
}

func (a *App) selectView(s viewSelection) {
	a.st.Cols, a.st.Rows = s.cols, s.rows
	s.display.apply(a.st)
	a.st.Active = 0
	for i, ws := range a.st.Workspaces {
		if ws.ID == s.workspace {
			a.st.Active = i
		}
		for j, t := range ws.Tabs {
			if t.ID == s.tabs[ws.ID] {
				ws.ActiveTab = j
				break
			}
		}
		if ws.ActiveTab >= len(ws.Tabs) {
			ws.ActiveTab = 0
		}
	}
}

func (a *App) NewViewer(out io.Writer, cols, rows int, detach func(string)) *Viewer {
	ui := *a
	ui.sizeViewer, ui.sizeViewers = nil, nil
	if a.toast != nil {
		toast := *a.toast
		ui.toast = &toast
	}
	ui.out, ui.detacher = out, detach
	ui.fr, ui.enc = &input.Framer{}, &render.Encoder{}
	ui.drawViewers, ui.tickViewers, ui.visiblePane = nil, nil, nil
	ui.scroll = map[int]int{}
	ui.focusHistory = nil
	ui.focusHistoryAt = 0
	ui.cm, ui.sel, ui.sep, ui.sb = nil, nil, nil, nil
	ui.paneCopies = nil
	ui.sidebarGesture, ui.tabGesture = nil, nil
	ui.dividerClicks = [2]*sidebarDividerClick{}
	ui.menu, ui.settings, ui.help = nil, nil, nil
	ui.hostPixelMouse, ui.hostPixelSupported = false, false
	s := a.selection()
	v := &Viewer{app: &ui, workspace: s.workspace, tabs: s.tabs, cols: cols, rows: rows, display: s.display, toastSequence: a.toastSequence}
	ui.viewerOwner, ui.viewerContext = a, v
	return v
}

func (v *Viewer) with(a *App, fn func(*App)) {
	previous := a.selection()
	wasViewer := a.viewerActive
	a.viewerActive = true
	v.app.st, v.app.sup = a.st, a.sup
	v.app.trackers, v.app.reported, v.app.sessions = a.trackers, a.reported, a.sessions
	v.app.decisionAttention = a.decisionAttention
	v.app.blockerObservations = a.blockerObservations
	v.app.agentStates, v.app.agentStateSeq = a.agentStates, a.agentStateSeq
	v.app.agentStateCounter = a.agentStateCounter
	v.app.fossils, v.app.visibleIdle = a.fossils, a.visibleIdle
	v.app.jobRows, v.app.jobDots = a.jobRows, a.jobDots
	v.app.workspaceGit, v.app.workspaceNames = a.workspaceGit, a.workspaceNames
	v.app.workspaceLabels = a.workspaceLabels
	v.app.metadataViews = a.metadataViews
	v.app.sessionCwds = a.sessionCwds
	v.app.editorFiles = a.editorFiles
	v.app.spin = a.spin
	if v.toastSequence != a.toastSequence {
		v.toastSequence = a.toastSequence
		v.app.toast = nil
		if a.toast != nil {
			toast := *a.toast
			v.app.toast = &toast
		}
	}
	a.selectView(viewSelection{v.workspace, v.tabs, v.cols, v.rows, v.display})
	defer func() {
		s := a.selection()
		v.workspace, v.tabs, v.cols, v.rows = s.workspace, s.tabs, s.cols, s.rows
		v.display = s.display
		a.selectView(previous)
		a.viewerActive = wasViewer
		a.reported, a.sessions = v.app.reported, v.app.sessions
		a.agentStates, a.agentStateSeq = v.app.agentStates, v.app.agentStateSeq
		a.agentStateCounter = v.app.agentStateCounter
		if v.app.quit {
			a.quit = true
		}
		if v.app.dirty {
			a.dirty = true
		}
	}()
	fn(v.app)
}

func (v *Viewer) Input(a *App, bytes []byte) {
	v.InputWithSidebarPanel(a, bytes, nil)
}

func (v *Viewer) InputWithSidebarPanel(a *App, bytes []byte, jobs *bool) {
	v.with(a, func(ui *App) {
		if jobs != nil && ui.jobsPanel != *jobs {
			ui.jobsPanel = *jobs
			ui.agentScroll = 0
		}
		for _, event := range ui.fr.Push(bytes) {
			v.onInput(ui, event)
		}
		v.pendingAt = time.Now()
	})
}

func (v *Viewer) Resize(a *App, cols, rows, xp, yp int) {
	var copySizes map[*copyMode]copySize
	v.with(a, func(ui *App) {
		copySizes = ui.copySizes()
	})
	v.cols, v.rows = cols, rows
	v.with(a, func(ui *App) {
		ui.enc.Reset()
		ui.SetCellPixels(xp, yp)
		if a.sizeViewer == v {
			ui.syncLayout()
		}
		ui.copyResizeAll(copySizes)
		ui.dirty = true
	})
	if a.sizeViewer == v {
		a.st.Cols, a.st.Rows = v.cols, v.rows
	}
}

func (v *Viewer) Begin(a *App, xp, yp int) {
	v.takeSize(a)
	v.with(a, func(ui *App) { ui.BeginHostMouse(xp, yp); ui.dirty = true })
	a.st.Cols, a.st.Rows = v.cols, v.rows
}

func (v *Viewer) Tick(a *App) {
	v.with(a, func(ui *App) { ui.expireToast(); ui.flushSidebarDividerClicks(time.Now()) })
	if v.app.sel != nil && v.app.sel.autoDir != 0 {
		v.with(a, func(ui *App) { ui.selectionAutoScroll(time.Now()) })
	}
	if len(v.heldKeys) > 0 && !time.Now().Before(v.focusUntil) {
		v.with(a, func(ui *App) { v.releaseKeys(ui) })
	}
	if !v.app.fr.Pending() || time.Since(v.pendingAt) < time.Duration(v.app.fr.TimeoutMs())*time.Millisecond {
		return
	}
	v.with(a, func(ui *App) {
		for _, event := range ui.fr.FlushTimeout() {
			v.onInput(ui, event)
		}
	})
}

func (v *Viewer) VisiblePane(a *App, pid int) bool {
	if v.app.windowBlurred {
		return false
	}
	for _, ws := range a.st.Workspaces {
		if ws.ID == v.workspace {
			for _, tab := range ws.Tabs {
				if tab.ID == v.tabs[ws.ID] {
					return tab.Layout.Focused() == pid
				}
			}
		}
	}
	return false
}

func (v *Viewer) Draw(a *App, panes map[int]*render.PaneSnap) {
	v.with(a, func(ui *App) { ui.sharedPaneSnaps = panes; ui.draw(); ui.sharedPaneSnaps = nil })
}

// SetViewerHooks diverts display work only in session server mode.
func (a *App) SetViewerHooks(draw, tick func(), visible func(int) bool) {
	a.drawViewers, a.tickViewers, a.visiblePane = draw, tick, visible
}

func (a *App) SessionPaneSnaps() map[int]*render.PaneSnap {
	return a.paneSnaps()
}

// MarkViewerSeen precedes the single session read used by all viewers.
func (a *App) MarkViewerSeen(pid int) {
	if tr := a.trackers[pid]; tr != nil {
		tr.Focus()
	}
	if rep, ok := a.reported[pid]; ok && rep.unseen {
		rep.unseen = false
		a.reported[pid] = rep
	}
}

func (a *App) InViewer() bool { return a.viewerActive }

func (v *Viewer) FollowSessionFocus(a *App) {
	s := a.selection()
	v.workspace, v.tabs = s.workspace, s.tabs
	v.with(a, func(ui *App) { ui.revealServerWorkspace(); ui.dirty = true })
}

func (v *Viewer) AcquireSize(a *App) { v.with(a, func(ui *App) { ui.syncLayout() }) }

// Paste keeps clipboard data out of the terminal framer and drops a stale
// delivery if its text field closed while the clipboard was being read.
func (v *Viewer) Paste(a *App, text string) {
	v.with(a, func(ui *App) {
		ui.syncLayout()
		if !ui.modalTextInput() {
			return
		}
		ui.insertClipboardText(text)
	})
	a.st.Cols, a.st.Rows = v.cols, v.rows
}
