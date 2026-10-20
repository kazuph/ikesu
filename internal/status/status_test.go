package status

import (
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/detect"
)

var base = time.Unix(0, 0)

func idle() detect.Detection    { return detect.Detection{State: detect.Idle} }
func working() detect.Detection { return detect.Detection{State: detect.Working} }
func blocked() detect.Detection { return detect.Detection{State: detect.Blocked} }
func visIdle() detect.Detection {
	return detect.Detection{State: detect.Idle, VisibleIdle: true}
}
func skipRule() detect.Detection {
	return detect.Detection{State: detect.Working, SkipStateUpdate: true}
}

// newWorkingTracker returns a tracker past startup grace, currently Working.
func newWorkingTracker(t *testing.T) (*Tracker, time.Time) {
	t.Helper()
	tr := NewTracker()
	tr.Update(working(), "claude", false, base) // agent identified
	t0 := base.Add(4 * time.Second)             // after grace
	tr.Update(working(), "claude", false, t0)
	if tr.State != detect.Working {
		t.Fatalf("setup: want working, got %v", tr.State)
	}
	return tr, t0
}

func TestWorkingToPlainIdleHeldThenPublished(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	if tr.Update(idle(), "claude", false, t0.Add(100*time.Millisecond)) {
		t.Fatal("should hold")
	}
	if tr.State != detect.Working {
		t.Fatalf("held state = %v", tr.State)
	}
	tr.Update(idle(), "claude", false, t0.Add(200*time.Millisecond))
	tr.Update(idle(), "claude", false, t0.Add(300*time.Millisecond))
	tr.Update(idle(), "claude", false, t0.Add(400*time.Millisecond))
	if tr.State != detect.Idle {
		t.Fatalf("want idle after confirmations, got %v", tr.State)
	}
	if tr.Seen {
		t.Fatal("working→idle must clear seen")
	}
}

func TestWorkingToIdlePublishesAfterCap(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	tr.Update(idle(), "claude", false, t0.Add(100*time.Millisecond)) // hold
	if tr.State != detect.Working {
		t.Fatal("should hold")
	}
	tr.Update(idle(), "claude", false, t0.Add(800*time.Millisecond)) // >700ms cap
	if tr.State != detect.Idle {
		t.Fatalf("cap should publish idle, got %v", tr.State)
	}
}

func TestVisibleIdleImmediate(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	tr.Update(visIdle(), "claude", false, t0.Add(50*time.Millisecond))
	if tr.State != detect.Idle || tr.Seen {
		t.Fatalf("visible idle immediate → %+v", tr)
	}
}

func TestBlockedAndProcessExitImmediate(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	tr.Update(blocked(), "claude", false, t0.Add(50*time.Millisecond))
	if tr.State != detect.Blocked {
		t.Fatalf("blocked immediate, got %v", tr.State)
	}
	tr.Update(idle(), "claude", true, t0.Add(60*time.Millisecond))
	if tr.State != detect.Idle {
		t.Fatalf("process exit idle immediate, got %v", tr.State)
	}
}

func TestAgentChangePublishesIdleAndGraces(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	if tr.Update(idle(), "codex", false, t0.Add(10*time.Millisecond)) != true {
		t.Fatal("agent change must publish")
	}
	if tr.State != detect.Idle || !tr.Seen {
		t.Fatalf("agent change → %+v", tr)
	}
	if tr.Update(working(), "codex", false, t0.Add(1*time.Second)) {
		t.Fatal("grace must suppress updates")
	}
	if tr.State != detect.Idle {
		t.Fatalf("grace state = %v", tr.State)
	}
	tr.Update(working(), "codex", false, t0.Add(4*time.Second))
	if tr.State != detect.Working {
		t.Fatalf("post-grace = %v", tr.State)
	}
}

func TestSkipStateUpdatePreserves(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	if tr.Update(skipRule(), "claude", false, t0.Add(500*time.Millisecond)) {
		t.Fatal("skip must not publish")
	}
	if tr.State != detect.Working {
		t.Fatalf("skip changed state to %v", tr.State)
	}
}

func TestSeenFocusCycle(t *testing.T) {
	tr, t0 := newWorkingTracker(t)
	tr.Update(blocked(), "claude", false, t0.Add(10*time.Millisecond))
	tr.Update(visIdle(), "claude", false, t0.Add(20*time.Millisecond))
	if tr.Seen {
		t.Fatal("blocked→idle must clear seen")
	}
	tr.Focus()
	if !tr.Seen {
		t.Fatal("focus must set seen")
	}
}

func TestKeysAndAttention(t *testing.T) {
	if Key(detect.Idle, false) != "done" || Key(detect.Idle, true) != "idle" ||
		Key(detect.Working, false) != "working" || Key(detect.Blocked, true) != "blocked" ||
		Key(detect.Unknown, false) != "unknown" {
		t.Fatal("key mapping wrong")
	}
	if !(Attention(detect.Blocked, false) > Attention(detect.Working, false) &&
		Attention(detect.Working, false) > Attention(detect.Idle, false) &&
		Attention(detect.Idle, false) > Attention(detect.Idle, true) &&
		Attention(detect.Idle, true) > Attention(detect.Unknown, true)) {
		t.Fatal("attention order wrong")
	}
	max := 0
	for _, p := range []struct {
		s    detect.State
		seen bool
	}{{detect.Unknown, true}, {detect.Idle, false}, {detect.Working, true}} {
		if a := Attention(p.s, p.seen); a > max {
			max = a
		}
	}
	if max != 3 {
		t.Fatalf("aggregate = %d, want working(3)", max)
	}
}
