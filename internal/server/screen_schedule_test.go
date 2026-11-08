package server

import (
	"testing"
	"time"
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
