package server

import (
	"os"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestScreenPromptMissingPaneCancelsBeforeAnswer(t *testing.T) {
	teststate.Lock(t)
	s, _, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	id := "1"
	var resp api.Response
	s.app.Do(func() {
		s.sup.Kill(1)
		resp = s.decisionRequest(&api.Request{ID: "closed", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &id})})
	})
	if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
		t.Fatal(resp)
	}
	got, err := s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatal(got, err)
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal(keys)
	}
}

func TestScreenPromptDamagedReadRequiresLastMatchedScan(t *testing.T) {
	teststate.Lock(t)
	for _, previousMatched := range []bool{true, false} {
		s, p, input := screenTerminal(t, "claude-single", "claude")
		scanScreen(s)
		d := screenRecord(t, s)
		paintScreen(p, "  1. incomplete option row")
		if !previousMatched {
			scanScreen(s)
		}
		resp := answerScreen(s, d, "1", nil)
		if previousMatched {
			if resp.Err != nil {
				t.Fatal(resp.Err)
			}
			waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "1" })
		} else {
			if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
				t.Fatal(resp)
			}
			if keys, _ := os.ReadFile(input); len(keys) != 0 {
				t.Fatal(keys)
			}
		}
	}
}
