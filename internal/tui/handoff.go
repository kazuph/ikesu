package tui

import (
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/status"
	"time"
)

type ReportTransfer struct {
	Agent, State string
	Unseen       bool
	Source       string
	At           time.Time
}
type HandoffState struct {
	State               *model.AppState
	Counters            model.HandoffCounters
	Sessions            map[int]AgentRecord
	Reported            map[int]ReportTransfer
	BlockerObservations map[int]blockerObservation
	Scroll              map[int]int
	Trackers            map[int]status.Transfer
	Fossils             map[int]FossilTransfer
	VisibleIdle         map[int]time.Time
	AgentStates         map[int]string
	AgentSequences      map[int]uint64
	AgentCounter        uint64
}

type FossilTransfer struct {
	Fingerprint string
	Since       time.Time
}

func (a *App) HandoffState() HandoffState {
	t := HandoffState{State: a.st, Counters: a.st.HandoffCounters(), Sessions: a.SessionRecords(), Reported: map[int]ReportTransfer{}, Scroll: a.scroll, BlockerObservations: a.blockerObservations}
	t.Trackers, t.Fossils = map[int]status.Transfer{}, map[int]FossilTransfer{}
	for id, tr := range a.trackers {
		t.Trackers[id] = tr.Transfer()
	}
	for id, f := range a.fossils {
		t.Fossils[id] = FossilTransfer{f.fp, f.since}
	}
	t.VisibleIdle, t.AgentStates, t.AgentSequences, t.AgentCounter = a.visibleIdle, a.agentStates, a.agentStateSeq, a.agentStateCounter
	for id, r := range a.reported {
		t.Reported[id] = ReportTransfer{Agent: r.agent, State: r.state, Unseen: r.unseen, Source: r.source, At: r.at}
	}
	return t
}
func (a *App) RestoreHandoff(t HandoffState) {
	a.st = t.State
	a.st.RestoreHandoffCounters(t.Counters)
	a.sessions = map[int]*AgentRecord{}
	for id, r := range t.Sessions {
		rec := r
		a.sessions[id] = &rec
	}
	a.reported = map[int]reportedPane{}
	for id, r := range t.Reported {
		a.reported[id] = reportedPane{agent: r.Agent, state: r.State, unseen: r.Unseen, source: r.Source, at: r.At}
	}
	a.scroll = t.Scroll
	a.blockerObservations = t.BlockerObservations
	a.trackers, a.fossils = map[int]*status.Tracker{}, map[int]*fossilWatch{}
	for id, tr := range t.Trackers {
		a.trackers[id] = status.Restore(tr)
	}
	for id, f := range t.Fossils {
		a.fossils[id] = &fossilWatch{f.Fingerprint, f.Since}
	}
	a.visibleIdle, a.agentStates, a.agentStateSeq, a.agentStateCounter = t.VisibleIdle, t.AgentStates, t.AgentSequences, t.AgentCounter
	a.dirty = true
}

// RetireHandoff exits the loop without invoking the normal quit effects.
func (a *App) RetireHandoff() { a.quit = true; a.quitHook = nil }
