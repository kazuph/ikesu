package tui

import (
	uv "github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/status"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestDetectionActivityQuietAndSingleTarget(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	ids := a.st.Workspaces[0].Tabs[0].Layout.PaneIDs()
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	a.detectAll()
	first, second := ids[0], ids[1]
	beforeFirst, beforeSecond := a.blockerObservations[first].At, a.blockerObservations[second].At
	now := time.Now().Add(tickInterval)
	a.detectChanged(now)
	if a.blockerObservations[first].At != beforeFirst || a.blockerObservations[second].At != beforeSecond {
		t.Fatal("quiet panes were detected again")
	}
	a.invalidateDetection(first)
	a.detectChanged(now)
	if !a.blockerObservations[first].At.Equal(now) || a.blockerObservations[second].At != beforeSecond {
		t.Fatal("changed pane did not remain the only target")
	}
	v := a.NewViewer(nil, 120, 35, nil)
	v.with(a, func(ui *App) { ui.invalidateDetection(second) })
	a.detectChanged(now.Add(tickInterval))
	if !a.blockerObservations[second].At.Equal(now.Add(tickInterval)) {
		t.Fatal("viewer invalidation did not reach the owner")
	}
}

func TestDetectionActivityKeepsGraceAndFossilDeadlines(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p := a.sup.Get(1)
	p.SetAgent("claude")
	p.WithEmu(func(e *uv.Emulator) {
		_, _ = e.Write([]byte("\x1b[2J\x1b[H──────────────\r\n✻ Thinking… (12s · esc to interrupt)\r\n──────────────\r\n❯ \r\n──────────────\r\n"))
	})
	a.detectAll()
	until := a.trackers[1].Transfer().GraceUntil
	if until.IsZero() || a.trackers[1].State != detect.Idle {
		t.Fatal("startup grace not established")
	}
	observed := a.blockerObservations[1].At
	a.detectChanged(until.Add(-time.Nanosecond))
	if a.blockerObservations[1].At != observed {
		t.Fatal("grace caused repeated quiet detection")
	}
	a.detectChanged(until)
	if a.trackers[1].State != detect.Working {
		t.Fatal("working evidence did not publish at the original grace deadline", a.trackers[1].State)
	}
	expiry := a.fossils[1].since.Add(fossilExpiry)
	a.detectChanged(expiry)
	if !a.trackers[1].Transfer().PendingActive {
		t.Fatal("fossil correction lost the existing idle confirmations")
	}
	for i := 1; i <= status.PendingIdleConfirmations; i++ {
		a.detectChanged(expiry.Add(time.Duration(i)*tickInterval - time.Nanosecond))
	}
	if a.trackers[1].State != detect.Idle {
		t.Fatal("quiet working evidence did not fossilize at its existing deadline", a.trackers[1].State)
	}
	if _, ok := a.paneActivity().deadlines[1]; ok {
		t.Fatal("expired fossil kept a periodic detection deadline")
	}
}
