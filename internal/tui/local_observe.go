package tui

import (
	"github.com/kazuph/ikesu/internal/session"
	"strings"
	"time"
)

func (a *App) observeLocalSessions() {
	if a.localObservedStart == nil {
		a.localObservedStart = map[int]time.Time{}
	}
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.Order() {
				rp := a.sup.Get(pid)
				if rp == nil {
					continue
				}
				snap := rp.Snapshot()
				if snap.AgentID == "" {
					continue
				}
				rec, hasRec := a.SessionRecord(pid)
				if hasRec && rec.Value != "" && strings.HasPrefix(rec.Source, "report:") {
					continue
				}
				if hasRec && rec.Agent == "codex" && rec.Kind == "id" && a.codexSessionInOtherPane(pid, rec.Value) {
					delete(a.localObservedStart, pid)
				}
				job := rp.ForegroundJob()
				start, hasStart := session.ProcessStart(job)
				if hasStart && hasRec && rec.Value != "" && a.localObservedStart[pid].Equal(start) {
					continue
				}
				if snap.AgentID == "codex" {
					if id, found := session.ObserveCmdline(job); found {
						if !a.codexSessionInOtherPane(pid, id) {
							a.RecordSession(pid, AgentRecord{Agent: snap.AgentID, Kind: "id", Value: id, Title: rec.Title, Source: "observed:cmdline"})
							if hasStart {
								a.localObservedStart[pid] = start
							}
						}
						continue
					}
				}
				if hasStart {
					if id, found := session.ObserveProcessSession(snap.AgentID, snap.Cwd, start, job, rp.SessionEnvironment()); found {
						if snap.AgentID == "codex" && a.codexSessionInOtherPane(pid, id) {
							continue
						}
						a.RecordSession(pid, AgentRecord{Agent: snap.AgentID, Kind: "id", Value: id, Title: rec.Title, Source: "observed:file"})
						a.localObservedStart[pid] = start
						continue
					}
				}
				if hasRec && rec.Value != "" {
					continue
				}
				if id, found := session.ObserveCmdline(job); found {
					a.RecordSession(pid, AgentRecord{Agent: snap.AgentID, Kind: "id", Value: id, Title: rec.Title, Source: "observed:cmdline"})
					if hasStart {
						a.localObservedStart[pid] = start
					}
				}
			}
		}
	}
}

func (a *App) codexSessionInOtherPane(pid int, id string) bool {
	for otherPID, rec := range a.SessionRecords() {
		if otherPID != pid && rec.Agent == "codex" && rec.Kind == "id" && rec.Value == id {
			return true
		}
	}
	return false
}
