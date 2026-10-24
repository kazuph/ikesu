package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/status"
	"os"
	"path/filepath"
	"time"
)

func (a *App) onInput(ev input.Event) {
	switch ev.Kind {
	case input.KindTerminalReport:
		a.hostMouseReport(ev.Report)
		a.hostThemeReport(ev.Report)
	case input.KindKeyRelease:
		if a.modalTextInput() && ev.Key.Code == 'v' && (ev.Key.Mod == uv.ModCtrl || ev.Key.Mod == uv.ModSuper) {
			return
		}
		a.sendPaneKey(ev.Key, ev.Raw, true)
	case input.KindKey:
		a.handleKey(ev.Key, ev.Raw)
	case input.KindPaste:
		if ev.Text == "" {
			return
		}
		if a.navigator != nil {
			if a.navigator.searching {
				a.navigator.query += ev.Text
				a.navigatorMove(0)
			}
			return
		}
		if a.settings != nil || a.menu != nil || a.help != nil {
			return
		}
		if a.dialog != nil {
			if d := a.dialog; d.input != nil {
				if d.machineForm {
					a.machineInputKey(uv.Key{Text: ev.Text})
					return
				}
				if d.replaceDefault {
					*d.input = ""
					d.replaceDefault = false
				}
				*d.input += ev.Text
				d.d.Input, d.d.InputCursor = *d.input, len([]rune(*d.input))
				a.dirty = true
			}
			return
		}
		if a.cm != nil {
			if a.cm.searching {
				a.insertClipboardText(ev.Text)
			}
			return
		}
		if a.workspaceNavigate {
			return
		}
		if pid := a.focusedPane(); pid >= 0 {
			if p := a.sup.Get(pid); p != nil {
				p.RecordExternalInput()
				p.WithEmu(func(e *vt.Emulator) { e.Paste(ev.Text) })
			}
		}
	case input.KindMouseClick, input.KindMouseRelease, input.KindMouseWheel, input.KindMouseMotion:
		if ev.Mouse.X < 0 || ev.Mouse.Y < 0 {
			return
		}
		if a.hostPixelMouse {
			pixel := ev.Mouse
			a.mousePixel = &pixel
			defer func() { a.mousePixel = nil }()
			ev.Mouse.X /= a.cellW
			ev.Mouse.Y /= a.cellH
		}
		if ev.Mouse.X >= a.st.Cols || ev.Mouse.Y >= a.st.Rows {
			return
		}
		a.onMouse(ev)
	case input.KindFocus:
		a.windowBlurred = false
		if a.sidebarConfig.UI.RedrawOnFocusGained == nil || *a.sidebarConfig.UI.RedrawOnFocusGained {
			a.enc.Reset()
			a.dirty = true
		}
		a.detectAll()
	case input.KindBlur:
		a.windowBlurred = true
	case input.KindUnknown:
	}
}

// onRuntimeEvent handles supervisor notifications.
func (a *App) onRuntimeEvent(ev runtime.Event) {
	switch ev.Kind {
	case runtime.EventClipboard:
		if a.clipboardHook != nil {
			a.clipboardHook(ev.Pane, ev.Data)
		}
	case runtime.EventChanged:
		if p := a.sup.Get(ev.Pane); p != nil {
			p.MarkChangedRead()
		}
		if a.outHook != nil {
			a.outHook(ev.Pane)
		}
		a.dirty = true
	case runtime.EventExited:
		if a.st.Overlay != nil && ev.Pane == a.st.Overlay.PaneID {
			a.exec(model.CmdCloseOverlay{})
			return
		}
		if a.st.PopupByPane(ev.Pane) != nil {
			a.exec(model.CmdClosePopup{Pane: ev.Pane})
			return
		}
		if a.exitedHook != nil {
			a.exitedHook(ev.Pane)
		}
		// A pane that still carries an agent identity and a resumable
		// session is kept as a shell-fallback pane instead of closing
		// (SPEC G3 restorable pane retention).
		if a.retainOnExit(ev.Pane) {
			a.respawnShell(ev.Pane)
		} else {
			a.exec(model.CmdPaneExited{Pane: ev.Pane})
		}
	}
}

// detectAll runs the detection/status pass over every live pane.
func (a *App) detectAll() {
	now := time.Now()
	activeWS := a.st.ActiveWorkspace()
	for _, ws := range a.st.Workspaces {
		focusedID := -1
		if l := ws.ActiveLayout(); l != nil {
			focusedID = l.Focused()
		}
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				p := a.sup.Get(pid)
				if p == nil {
					continue
				}
				tr := a.trackers[pid]
				if tr == nil {
					tr = status.NewTracker()
					a.trackers[pid] = tr
				}
				sn := p.Snapshot()
				agentID := sn.AgentID
				in := detect.Input{OSCTitle: sn.OSCTitle, OSCProgress: sn.OSCProgress}
				if agentID != "" {
					in.Screen = p.ScreenText()
				}
				before := a.glyph(pid).Key
				d := detect.Detect(agentID, in)
				d = a.applyFossilExpiry(pid, agentID, in.Screen, d, now)
				a.observeBlocker(pid, agentID, d, now)
				a.clearStaleReport(pid, agentID, d, now)
				trackerChanged := tr.Update(d, agentID, sn.Dead, now)
				if trackerChanged || before != a.glyph(pid).Key {
					a.dirty = true
					if a.availHook != nil {
						a.availHook(pid)
					}
				}
				a.noteAgentState(pid)
				// The focused pane in the visible workspace is seen.
				if a.visiblePane != nil && a.visiblePane(pid) || a.visiblePane == nil && ws == activeWS && pid == focusedID && !a.windowBlurred {
					if rep := a.reported[pid]; rep.unseen && (a.sinkOn == nil || a.sinkOn()) {
						rep.unseen = false
						a.reported[pid] = rep
						a.dirty = true
					}
					if !tr.Seen {
						tr.Focus()
						a.dirty = true
					}
				}
			}
		}
	}
	// Drop trackers for panes that no longer exist.
	for pid := range a.trackers {
		if a.sup.Get(pid) == nil {
			delete(a.blockerObservations, pid)
			delete(a.trackers, pid)
			delete(a.reported, pid)
		}
	}
}

// copiedFlash returns the " Copied N lines " text while inside its 2s
// window. Checked at draw time too so a flash left over while no client
// was attached (tick suspended) never leaks into a restored session
// (SPEC: deadline 経過後に残らない).
func (a *App) copiedFlash() string {
	return a.copiedFlashAt(time.Now())
}

func (a *App) copiedFlashAt(now time.Time) string {
	if enabled := a.sidebarConfig.UI.Toast.Clipboard.Enabled; enabled != nil && !*enabled {
		return ""
	}
	if a.copied != "" && now.Sub(a.copiedAt) < copiedDuration {
		return a.copied
	}
	return ""
}

// onTick handles periodic work: spinner animation, copied-flash expiry,
// foreground-process polling, and detection.
func (a *App) onTick() {
	if a.tickViewers != nil {
		a.tickViewers()
	}
	now := time.Now()
	a.flushSidebarDividerClicks(now)
	if a.localRestore {
		a.observeLocalSessions()
	}
	a.refreshPaneMetadata()
	a.refreshWorkspaceGit(now)
	a.expireToast()
	if a.copied != "" && now.Sub(a.copiedAt) >= copiedDuration {
		a.copied = ""
		a.dirty = true
	}
	// Foreground process poll: every ~1s per pane.
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if p := a.sup.Get(pid); p != nil {
					p.PollProcess()
				}
			}
		}
	}
	a.popupSelectionAutoScroll()
	a.selectionAutoScroll(now)
	a.refreshJobDots(now)
	a.detectAll()
}

func (a *App) anyWorking() bool {
	for _, ws := range a.st.Workspaces {
		for _, t := range ws.Tabs {
			for _, pid := range t.Layout.PaneIDs() {
				if g := a.glyph(pid); g.Key == "working" {
					return true
				}
			}
		}
	}
	return false
}

// exec applies a command and performs its effects.
func (a *App) exec(cmd model.Command) {
	if c, ok := cmd.(model.CmdCloseWorkspace); ok && c.Index == 0 && len(a.st.Workspaces) == 1 {
		home, err := os.UserHomeDir()
		if err != nil {
			a.lastSpawnError = err
			a.ShowToast("workspace close failed", err.Error(), "error", a.focusedPane())
			return
		}
		a.lastSpawnError = nil
		a.exec(model.CmdNewWorkspace{Cwd: home, Name: filepath.Base(home)})
		if a.lastSpawnError != nil {
			a.exec(model.CmdCloseWorkspace{Index: 1})
			a.ShowToast("workspace close failed", a.lastSpawnError.Error(), "error", a.focusedPane())
			return
		}
		delete(a.workspaceNames, 0)
		delete(a.workspaceGit, 0)
	}

	policyCwd := false
	switch c := cmd.(type) {
	case model.CmdSplitFocused:
		policyCwd = true
	case model.CmdNewTab:
		policyCwd = c.Cwd == "" && len(c.Argv) == 0
	}

	switch cmd.(type) {
	case model.CmdSelectTab, model.CmdNewTab, model.CmdRenameTab, model.CmdSelectWorkspace, model.CmdNewWorkspace, model.CmdResize:
		a.tabScrollManual = false
	case model.CmdSetSection, model.CmdToggleSection:
		a.sidebarScroll = 0
		a.mobileScroll = 0
	}
	beforeFocus := a.focusedPane()
	spawnCwd := ""
	if policyCwd {
		if p := a.sup.Get(beforeFocus); p != nil {
			p.PollProcess()
			spawnCwd = p.SpawnCwd()
		}
	}
	var metadataOrder []int
	if _, moving := cmd.(model.CmdMoveWorkspace); moving {
		for _, ws := range a.st.Workspaces {
			metadataOrder = append(metadataOrder, ws.ID)
		}
	}
	var copySizes map[*copyMode]copySize
	if _, resizing := cmd.(model.CmdResize); resizing {
		copySizes = a.copySizes()
	}
	popupWasVisible := a.st.PopupVisible()
	previousPopup := a.st.Popup
	var prev *model.StateSnap
	if a.postExecHook != nil {
		prev = a.st.Capture()
	}
	for _, e := range a.st.Apply(cmd) {
		if spawn, ok := e.(model.EffSpawnPane); ok && policyCwd {
			if spawnCwd != "" {
				spawn.Cwd = spawnCwd
			}
			spawn.Cwd = a.newPaneCwd(spawn.Cwd)
			e = spawn
		}
		a.effect(e)
	}
	if metadataOrder != nil {
		a.reindexWorkspaceMetadata(metadataOrder)
	}
	switch cmd.(type) {
	case model.CmdToggleSidebar, model.CmdToggleRightSidebar:
		a.saveViewPreferences()
	}
	a.popupVisibilityChanged(popupWasVisible, previousPopup)
	a.recordFocus(beforeFocus, a.focusedPane())
	if _, selecting := cmd.(model.CmdSelectWorkspace); selecting && a.serverUI {
		a.revealServerWorkspace()
	}
	if creating, ok := cmd.(model.CmdNewWorkspace); ok && !creating.NoFocus && a.st.WorkspaceDensity == "slim" {
		a.revealServerWorkspace()
	}
	// Copy mode dies when its pane leaves the active tab's layout
	// (closed, moved, or the tab/workspace changed).
	if a.cm != nil {
		ws := a.st.ActiveWorkspace()
		alive := a.st.OverlayVisible() && a.st.Overlay.PaneID == a.cm.pane || a.st.PopupVisible() && a.st.Popup.PaneID == a.cm.pane || ws != nil && ws.ActiveLayout() != nil && ws.ActiveLayout().HasPane(a.cm.pane)
		if !alive {
			a.exitCopy()
		}
	}
	a.copyFocusChanged()
	a.dirty = true
	// Keystrokes and resizes never change persisted state; everything else
	// (splits, closes, focus, renames, tabs, workspaces) schedules a save.
	switch cmd.(type) {
	case model.CmdInput, model.CmdResize, model.CmdOpenOverlay, model.CmdCloseOverlay, model.CmdOpenPopup, model.CmdClosePopup, model.CmdPopupResizeLock:
		if _, resized := cmd.(model.CmdResize); resized {
			a.copyResizeAll(copySizes)
		}
	default:
		a.changed()
	}
	if a.postExecHook != nil && prev != nil {
		a.postExecHook(cmd, prev)
	}
}

func (a *App) effect(e model.Effect) {
	switch e := e.(type) {
	case model.EffSpawnPane:
		r := a.ptyRectForPane(e.Pane)
		var env []string
		for k, v := range e.Env {
			env = append(env, k+"="+v)
		}
		spawn := a.sup.SpawnWith
		if a.st.PopupByPane(e.Pane) != nil {
			spawn = a.sup.SpawnPopup
		}
		if e.AgentCommand {
			spawn = a.sup.SpawnAgent
		}
		if _, err := spawn(e.Pane, e.Cwd, r.W, r.H, env, e.Argv); err != nil {
			a.lastSpawnError = err
		}
	case model.EffKillPane:
		a.forgetClosedSizeTabs()
		a.removeEditorFile(e.Pane)
		a.clearPopupInteraction(e.Pane)
		a.sup.Kill(e.Pane)
		delete(a.scroll, e.Pane)
		delete(a.reported, e.Pane)
		delete(a.agentStates, e.Pane)
		delete(a.agentStateSeq, e.Pane)
		delete(a.sessions, e.Pane)
		delete(a.localObservedStart, e.Pane)
		if a.paneClosedHook != nil {
			a.paneClosedHook(e.Pane)
		}
	case model.EffSyncLayout:
		a.forgetClosedSizeTabs()
		a.syncLayout()
	case model.EffInput:
		a.sup.Input(e.Pane, e.Bytes)
	case model.EffQuit:
		a.quit = true
	}
}

// syncLayout applies the last active display geometry to the visible tab.
func (a *App) syncLayout() {
	a.syncPaneLayout()
}

func (a *App) syncPaneLayout() {
	previous := a.selection()
	defer a.selectView(previous)
	for wi, ws := range a.st.Workspaces {
		if a.viewerOwner != nil && ws.ID != previous.workspace {
			continue
		}
		a.st.Active = wi
		for ti, tab := range ws.Tabs {
			if a.viewerOwner != nil && tab.ID != previous.tabs[ws.ID] {
				continue
			}
			ws.ActiveTab = ti
			a.withTabPTYGeometry(tab, func() {
				for _, pr := range a.ptyPaneRects(tab, a.st.PanesArea()) {
					w, h := model.PaneTermSize(pr.Rect, a.st.PaneBorders)
					a.sup.ResizeWithPixels(pr.ID, w, h, a.cellW, a.cellH)
				}
				if a.st.OverlayVisible() {
					r := a.innerRect(a.st.Overlay.PaneID)
					a.sup.ResizeWithPixels(a.st.Overlay.PaneID, r.W, r.H, a.cellW, a.cellH)
				}
				if a.st.PopupVisible() && !a.st.Popup.ResizeLocked {
					r := a.innerRect(a.st.Popup.PaneID)
					a.sup.ResizeWithPixels(a.st.Popup.PaneID, r.W, r.H, a.cellW, a.cellH)
				}
			})
		}
	}
}

// rectForPane returns the terminal-content rect of a pane in whichever
// workspace owns it, including the workspace's own tab bar geometry.
func (a *App) rectForPane(pid int) layout.Rect {
	previous := a.selection()
	defer a.selectView(previous)
	if a.st.Overlay != nil && pid == a.st.Overlay.PaneID {
		if r, ok := a.st.OverlayRect(); ok {
			inner, gutter := model.PaneContentRect(r, a.st.PaneBorders)
			inner.W = max(inner.W-gutter, 1)
			return inner
		}
	}
	if a.st.Popup != nil && pid == a.st.Popup.PaneID {
		return popupInner(a.st.PopupRect())
	}
	for wi, ws := range a.st.Workspaces {
		a.st.Active = wi
		for _, t := range ws.Tabs {
			for _, pr := range a.ptyPaneRects(t, a.st.PanesArea()) {
				if pr.ID == pid {
					inner, gutter := model.PaneContentRect(pr.Rect, a.st.PaneBorders)
					inner.W = max(inner.W-gutter, 1)
					return inner
				}
			}
		}
	}
	return layout.Rect{W: 80, H: 24}
}

// innerRect returns the content rect (inside the border) of a pane of the
// active workspace.
func (a *App) innerRect(pid int) layout.Rect {
	if a.st.Overlay != nil && pid == a.st.Overlay.PaneID {
		if r, ok := a.st.OverlayRect(); ok {
			inner, gutter := model.PaneContentRect(r, a.st.PaneBorders)
			inner.W = max(inner.W-gutter, 1)
			return inner
		}
	}
	if a.st.Popup != nil && pid == a.st.Popup.PaneID {
		return popupInner(a.st.PopupRect())
	}
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.Rect{W: 80, H: 24}
	}
	t := ws.TabRef()
	if t == nil {
		return layout.Rect{W: 80, H: 24}
	}
	for _, pr := range a.st.PaneRectsForDisplay(t, a.st.PanesArea()) {
		if pr.ID == pid {
			inner, gutter := model.PaneContentRect(pr.Rect, a.st.PaneBorders)
			inner.W = max(inner.W-gutter, 1)
			return inner
		}
	}
	return layout.Rect{W: 80, H: 24}
}

// paneRect returns the outer (bordered) rect of a pane of the active ws.
func (a *App) paneRect(pid int) (layout.Rect, bool) {
	if a.st.OverlayVisible() && pid == a.st.Overlay.PaneID {
		return a.st.OverlayRect()
	}
	if a.st.PopupVisible() && pid == a.st.Popup.PaneID {
		return a.st.PopupRect(), true
	}
	ws := a.st.ActiveWorkspace()
	if ws == nil {
		return layout.Rect{}, false
	}
	t := ws.TabRef()
	if t == nil {
		return layout.Rect{}, false
	}
	for _, pr := range a.st.PaneRectsForDisplay(t, a.st.PanesArea()) {
		if pr.ID == pid {
			return pr.Rect, true
		}
	}
	return layout.Rect{}, false
}
