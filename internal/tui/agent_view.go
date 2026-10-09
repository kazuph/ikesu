package tui

import "github.com/kazuph/goterrace/internal/agentview"

func (a *App) SetAgentView(view *agentview.View) { a.st.AgentView = view; a.dirty = true }
func (a *App) noteAgentState(pid int) {
	key := a.glyph(pid).Key
	previous := "unknown"
	if a.agentStates != nil {
		if value, ok := a.agentStates[pid]; ok {
			previous = value
		}
	}
	if key == previous {
		return
	}
	if a.agentStates == nil {
		a.agentStates = map[int]string{}
		a.agentStateSeq = map[int]uint64{}
	}
	a.agentStateCounter++
	a.agentStates[pid] = key
	a.agentStateSeq[pid] = a.agentStateCounter
}
func (a *App) AgentStateChangeSeq(pid int) uint64 { a.noteAgentState(pid); return a.agentStateSeq[pid] }
