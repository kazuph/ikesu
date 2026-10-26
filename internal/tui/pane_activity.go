package tui

import (
	"github.com/kazuph/ikesu/internal/benchstage"
	"time"
)

type paneActivity struct {
	initialized bool
	pending     map[int]bool
	confirming  map[int]bool
	deadlines   map[int]time.Time
	next        time.Time
}

func (a *App) paneActivity() *paneActivity {
	if a.viewerOwner != nil {
		return a.viewerOwner.paneActivity()
	}
	if a.activity == nil {
		a.activity = &paneActivity{pending: map[int]bool{}, confirming: map[int]bool{}, deadlines: map[int]time.Time{}}
	}
	return a.activity
}

func (a *App) invalidateDetection(pid int) {
	a.paneActivity().pending[pid] = true
}

func (a *App) scheduleDetection(pid int, now time.Time) {
	state := a.paneActivity()
	delete(state.deadlines, pid)
	delete(state.confirming, pid)
	add := func(at time.Time) {
		if !at.After(now) {
			return
		}
		if previous := state.deadlines[pid]; previous.IsZero() || at.Before(previous) {
			state.deadlines[pid] = at
		}
	}
	if tracker := a.trackers[pid]; tracker != nil {
		t := tracker.Transfer()
		if t.GraceActive {
			add(t.GraceUntil)
		}
		if t.PendingActive {
			// Confirm on each existing tick; adding 50ms to an actual wake time
			// could skip the next tick when its scheduling jitter is smaller.
			state.confirming[pid] = true
		}
	}
	if f := a.fossils[pid]; f != nil {
		add(f.since.Add(fossilExpiry))
	}
	if since := a.visibleIdle[pid]; !since.IsZero() {
		add(since.Add(fossilExpiry))
	}
}

func (a *App) nextDetectionDeadline() {
	state := a.paneActivity()
	state.next = time.Time{}
	for _, at := range state.deadlines {
		if state.next.IsZero() || at.Before(state.next) {
			state.next = at
		}
	}
}

func (a *App) detectChanged(now time.Time) {
	state := a.paneActivity()
	if !state.initialized {
		a.detectAll()
		return
	}
	for pid := range state.confirming {
		state.pending[pid] = true
	}
	if !state.next.IsZero() && !now.Before(state.next) {
		for pid, at := range state.deadlines {
			if !now.Before(at) {
				state.pending[pid] = true
			}
		}
	}
	if len(state.pending) == 0 {
		return
	}
	defer benchstage.Span("ui.detect")()
	for pid := range state.pending {
		delete(state.pending, pid)
		wi, _ := a.st.FindPane(pid)
		if a.sup.Get(pid) == nil {
			delete(state.deadlines, pid)
			delete(state.confirming, pid)
			delete(a.blockerObservations, pid)
			delete(a.trackers, pid)
			delete(a.reported, pid)
			continue
		}
		if wi < 0 {
			delete(state.deadlines, pid)
			delete(state.confirming, pid)
			continue
		}
		ws := a.st.Workspaces[wi]
		focused := ws.ActiveLayout() != nil && ws.ActiveLayout().Focused() == pid
		visible := a.visiblePane != nil && a.visiblePane(pid) || a.visiblePane == nil && ws == a.st.ActiveWorkspace() && focused && !a.windowBlurred
		a.detectPane(pid, visible, now)
	}
	a.nextDetectionDeadline()
}
