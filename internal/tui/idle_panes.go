package tui

import (
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
)

func (v *Viewer) IdlePanes(a *App) endpoint.IdlePanes {
	var list endpoint.IdlePanes
	v.with(a, func(ui *App) { list = ui.IdlePanes() })
	return list
}

// IdlePanes captures data on the server's app loop and uses its clock.
func (a *App) IdlePanes() endpoint.IdlePanes {
	result := endpoint.IdlePanes{}
	count := 0
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			count += tab.Layout.PaneCount()
		}
	}
	now := time.Now()
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, id := range tab.Layout.PaneIDs() {
				p := a.sup.Get(id)
				if p == nil {
					continue
				}
				agent := agentFor(p.Snapshot())
				if report := a.reported[id]; report.agent != "" {
					agent = report.agent
				}
				if agent == "" {
					continue
				}
				if count == 1 {
					result.LastPaneKept = true
					continue
				}
				at, seq := p.IdleActivity()
				result.Panes = append(result.Panes, endpoint.IdlePane{ID: id, Generation: a.st.PaneGeneration(id), ActivitySeq: seq, StateSeq: a.AgentStateChangeSeq(id), State: a.glyph(id).Key, Agent: agent, Workspace: ws.ID, WorkspaceGeneration: ws.Generation, WorkspaceName: ws.Name, TabName: tab.Name, AgeNanos: int64(now.Sub(at)), PendingInput: p.HasUnsentAgentInput(), Focused: id == a.focusedPane()})
			}
		}
	}
	return result
}

// DeleteIdlePanes validates and closes on the app loop, excluding concurrent input.
func (a *App) DeleteIdlePanes(panes []endpoint.IdlePane) endpoint.IdleDeleteResult {
	result := endpoint.IdleDeleteResult{}
	for _, candidate := range panes {
		p := a.sup.Get(candidate.ID)
		if p == nil {
			result.Failed++
			continue
		}
		ok := p.WithIdleActivitySequence(candidate.ActivitySeq, func() bool {
			wi, _ := a.st.FindPane(candidate.ID)
			if wi < 0 || a.st.PaneGeneration(candidate.ID) != candidate.Generation || a.AgentStateChangeSeq(candidate.ID) != candidate.StateSeq || a.glyph(candidate.ID).Key != candidate.State {
				return false
			}
			count := 0
			for _, ws := range a.st.Workspaces {
				for _, tab := range ws.Tabs {
					count += tab.Layout.PaneCount()
				}
			}
			if count <= 1 {
				return false
			}
			a.exec(model.CmdClosePane{Pane: candidate.ID})
			return true
		})
		if ok {
			result.Deleted++
		} else {
			result.Failed++
		}
	}
	return result
}
