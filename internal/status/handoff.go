package status

import (
	"github.com/kazuph/goterrace/internal/detect"
	"time"
)

type Transfer struct {
	AgentID                    string
	State                      detect.State
	Seen                       bool
	PendingStart, GraceUntil   time.Time
	PendingCount               int
	PendingActive, GraceActive bool
}

func (t *Tracker) Transfer() Transfer {
	return Transfer{t.AgentID, t.State, t.Seen, t.pendingStart, t.graceUntil, t.pendingCount, t.pendingActive, t.graceActive}
}
func Restore(t Transfer) *Tracker {
	return &Tracker{AgentID: t.AgentID, State: t.State, Seen: t.Seen, pendingStart: t.PendingStart, graceUntil: t.GraceUntil, pendingCount: t.PendingCount, pendingActive: t.PendingActive, graceActive: t.GraceActive}
}
