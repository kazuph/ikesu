package tui

import "github.com/kazuph/goterrace/internal/model"

type ReportTransfer struct {
	Agent, State string
	Unseen       bool
}
type HandoffState struct {
	State    *model.AppState
	Counters model.HandoffCounters
	Sessions map[int]AgentRecord
	Reported map[int]ReportTransfer
	Scroll   map[int]int
}

func (a *App) HandoffState() HandoffState {
	t := HandoffState{State: a.st, Counters: a.st.HandoffCounters(), Sessions: a.SessionRecords(), Reported: map[int]ReportTransfer{}, Scroll: a.scroll}
	for id, r := range a.reported {
		t.Reported[id] = ReportTransfer{r.agent, r.state, r.unseen}
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
		a.reported[id] = reportedPane{r.Agent, r.State, r.Unseen}
	}
	a.scroll = t.Scroll
	a.dirty = true
}

// RetireHandoff exits the loop without invoking the normal quit effects.
func (a *App) RetireHandoff() { a.quit = true; a.quitHook = nil }
