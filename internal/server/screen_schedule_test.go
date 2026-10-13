package server

import (
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/teststate"
)

func TestScreenPromptWaitingAndPreviewIntervals(t *testing.T) {
	s := &Server{}
	if got := s.screenPromptPollInterval(); got != time.Second {
		t.Fatalf("blocked without a question: %v", got)
	}
	w := &screenPromptWatch{decisionID: "pending"}
	s.screenPrompts.watches = map[int]*screenPromptWatch{1: w}
	if got := s.screenPromptPollInterval(); got != time.Second {
		t.Fatalf("waiting for an answer: %v", got)
	}
	w.preview = &screenPreviewScan{}
	if got := s.screenPromptPollInterval(); got != eventWaitPoll {
		t.Fatalf("preview navigation: %v", got)
	}
	// A reservation must return without consulting panes or the app.
	s.screenPrompts.timer = time.NewTimer(time.Hour)
	defer s.screenPrompts.timer.Stop()
	s.scheduleScreenPromptPane(1)
}

func TestScreenPromptOutputReservationAndAppearanceLatency(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	start := time.Now()
	s.app.Do(func() {
		s.scheduleScreenPromptPane(999)
		if s.screenPrompts.timer != nil {
			t.Error("unrelated output scheduled a scan of blocked pane 1")
		}
		s.scheduleScreenPromptPane(1)
		reserved := s.screenPrompts.timer
		for i := 0; i < 100; i++ {
			s.scheduleScreenPromptPane(1)
			s.scheduleScreenPromptPane(999)
		}
		if s.screenPrompts.timer != reserved {
			t.Error("output replaced the existing reservation")
		}
	})
	deadline := start.Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		created := false
		s.app.Do(func() {
			w := s.screenPrompts.watches[1]
			created = w != nil && w.decisionID != ""
		})
		if created {
			t.Logf("question appeared to decision creation: %v", time.Since(start))
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("question was not converted to a decision within 1.5 seconds")
}
