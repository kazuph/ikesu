package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"time"
)

// SPEC G9 defines the startup settling window and launch deadline.
const agentLaunchSettle = 3 * time.Second
const agentLaunchDeadline = 30 * time.Second

type agentLaunch struct {
	started    time.Time
	readySince time.Time
}

// Called only on the app loop. Reported hooks and foreground detection both
// feed the same readiness state; a wrapper is not ready merely because it spawned.
func (s *Server) agentReadiness(pid int, agent string, status api.AgentStatus, dead bool) (pending, ready bool) {
	launch := s.agentLaunches[pid]
	if dead {
		delete(s.agentLaunches, pid)
		return false, false
	}
	if launch == nil {
		return false, agent != "" && status != api.AgentUnknown && status != api.AgentBlocked
	}
	if status == api.AgentBlocked {
		launch.readySince = time.Time{}
		return false, false
	}
	now := time.Now()
	if now.Sub(launch.started) >= agentLaunchDeadline {
		return false, false
	}
	if agent == "" || status == api.AgentUnknown {
		launch.readySince = time.Time{}
		return true, false
	}
	if launch.readySince.IsZero() {
		launch.readySince = now
	}
	if now.Sub(launch.readySince) < agentLaunchSettle {
		return true, false
	}
	delete(s.agentLaunches, pid)
	return false, true
}
