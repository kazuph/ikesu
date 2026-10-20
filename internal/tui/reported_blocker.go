package tui

import (
	"time"

	"github.com/kazuph/ikesu/internal/detect"
)

// Observations belong to the live runtime, not the saved agent report.
type blockerObservation struct {
	Agent   string
	At      time.Time
	Visible bool
}

func (a *App) observeBlocker(pid int, agent string, d detect.Detection, at time.Time) {
	if a.blockerObservations == nil {
		a.blockerObservations = map[int]blockerObservation{}
	}
	a.blockerObservations[pid] = blockerObservation{agent, at, d.State == detect.Blocked && d.VisibleBlocker}
}

func (a *App) reportedVisibleBlocker(pid int) bool {
	r, ok := a.reported[pid]
	if !ok || r.state == "" || r.state == "blocked" || fullLifecycleReport(r.source, r.agent) {
		return false
	}
	d := a.blockerObservations[pid]
	return d.Visible && d.Agent == r.agent && d.At.After(r.at)
}

func fullLifecycleReport(source, agent string) bool {
	switch agent {
	case "pi", "omp", "mastracode", "hermes", "opencode", "kilo", "kimi":
		return source == "gtx:"+agent
	}
	return false
}
