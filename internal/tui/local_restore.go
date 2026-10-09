package tui

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/session"
)

// restoreLocalAgents uses only this app's exact pane-session associations.
func (a *App) restoreLocalAgents(dryRun bool) {
	a.observeLocalSessions()
	launched, skipped, running := 0, 0, 0
	for _, ws := range a.st.Workspaces {
		for _, tab := range ws.Tabs {
			for _, pid := range tab.Layout.Order() {
				rp := a.sup.Get(pid)
				if rp == nil {
					continue
				}
				if rp.Snapshot().AgentID != "" {
					running++
					continue
				}
				rec, ok := a.SessionRecord(pid)
				if !ok || rec.Agent == "" {
					continue
				}
				command, ok := session.ResumeCommand(a.sidebarConfig, rec.Agent, rec.Kind, rec.Value)
				if !ok {
					skipped++
					continue
				}
				if !dryRun {
					input := []byte(command + "\r")
					if written, err := rp.WriteToPTY(input); err != nil || written != len(input) {
						skipped++
						continue
					}
				}
				launched++
			}
		}
	}
	body := "no pending restore"
	if launched+skipped > 0 {
		verb := "launched"
		if dryRun {
			verb = "would launch"
		}
		body = fmt.Sprintf("%s %d, skipped %d", verb, launched, skipped)
	} else if running > 0 {
		body = fmt.Sprintf("no pending restore, %d already running", running)
	}
	a.ShowToast("agent restore", body, "info", -1)
}
