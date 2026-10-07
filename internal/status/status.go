// Package status stabilizes detection results into per-pane agent status:
// pending-idle confirmation, startup grace, and the seen/unseen flag that
// drives sidebar icons. All time values are passed in — nothing reads the
// clock internally, so tests are deterministic.
package status

import (
	"time"

	"github.com/kazuph/goterrace/internal/detect"
)

const (
	// PendingIdleConfirmations is the number of consecutive idle
	// observations (at ~100ms intervals) required to publish
	// Working → non-visible Idle.
	PendingIdleConfirmations = 3
	// PendingIdleCap publishes the pending idle after this much time even
	// with fewer observations.
	PendingIdleCap = 700 * time.Millisecond
	// StartupGrace suppresses Unknown → Idle settling right after an agent
	// is identified (herdr's AGENT_STARTUP_GRACE_WINDOW).
	StartupGrace = 3 * time.Second
)

// Tracker holds one pane's stabilized status.
type Tracker struct {
	AgentID string
	State   detect.State
	Seen    bool // false = finished but not yet looked at ("done")

	pendingStart  time.Time
	pendingCount  int
	pendingActive bool
	graceUntil    time.Time
	graceActive   bool
}

// NewTracker starts Unknown, not yet seen-worthy.
func NewTracker() *Tracker {
	return &Tracker{State: detect.Unknown, Seen: true}
}

// Update folds one detection result into the status. agentID is the
// currently identified agent ("" = none). processExited marks a foreground
// process exit. Returns true when the published state changed.
func (t *Tracker) Update(d detect.Detection, agentID string, processExited bool, now time.Time) bool {
	prev := t.State
	changedAgent := agentID != t.AgentID

	if changedAgent {
		// New foreground agent: publish Idle immediately and start grace.
		// Agent gone ("") → Unknown.
		t.AgentID = agentID
		if agentID != "" {
			t.State = detect.Idle
			t.graceUntil = now.Add(StartupGrace)
			t.graceActive = true
		} else {
			t.State = detect.Unknown
			t.graceActive = false
		}
		t.Seen = true
		t.clearPending()
		return true
	}

	if processExited {
		t.clearPending()
		t.graceActive = false
		t.apply(d)
		return t.State != prev
	}

	// Startup grace: hold detections while a fresh agent settles.
	if t.graceActive {
		if now.Before(t.graceUntil) {
			t.clearPending()
			return false
		}
		t.graceActive = false
		t.clearPending()
	}

	if d.SkipStateUpdate {
		return false
	}

	// Working → plain Idle needs confirmation.
	if t.shouldHold(d, now) {
		return false
	}
	t.apply(d)
	return t.State != prev
}

// shouldHold implements PendingIdleConfirmation: hold Working →
// non-visible Idle until 3 confirmations or 700ms.
func (t *Tracker) shouldHold(d detect.Detection, now time.Time) bool {
	hold := t.State == detect.Working &&
		d.State == detect.Idle &&
		!d.VisibleIdle && !d.VisibleBlocker
	if !hold {
		t.clearPending()
		return false
	}
	if !t.pendingActive {
		t.pendingActive = true
		t.pendingStart = now
		t.pendingCount = 0
		return true
	}
	if now.Sub(t.pendingStart) >= PendingIdleCap {
		t.clearPending()
		return false
	}
	t.pendingCount++
	if t.pendingCount >= PendingIdleConfirmations {
		t.clearPending()
		return false
	}
	return true
}

func (t *Tracker) apply(d detect.Detection) {
	from := t.State
	t.State = d.State
	if !d.Correction && (from == detect.Working || from == detect.Blocked) && d.State == detect.Idle {
		t.Seen = false
	}
}

func (t *Tracker) clearPending() {
	t.pendingActive = false
	t.pendingCount = 0
}

// Focus marks the pane seen — called when it is focused in the visible
// workspace.
func (t *Tracker) Focus() {
	t.Seen = true
}

// Key is the sidebar display key: done/idle/working/blocked/unknown.
func (t *Tracker) Key() string {
	return Key(t.State, t.Seen)
}

// Key maps (state, seen) to a display key.
func Key(state detect.State, seen bool) string {
	switch state {
	case detect.Idle:
		if seen {
			return "idle"
		}
		return "done"
	case detect.Working:
		return "working"
	case detect.Blocked:
		return "blocked"
	}
	return "unknown"
}

// Attention ranks statuses for workspace aggregation.
func Attention(state detect.State, seen bool) int {
	switch {
	case state == detect.Blocked:
		return 4
	case state == detect.Idle && !seen:
		return 3
	case state == detect.Working:
		return 2
	case state == detect.Idle:
		return 1
	}
	return 0
}
