package dispatch

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func decisionParams() api.DecisionCreateParams {
	return api.DecisionCreateParams{Kind: "ask", Title: "Proceed?", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}, {ID: "no", Label: "No", Role: "reject"}}}
}
func TestDecisionRecordPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := decisionParams()
	body := "body"
	p.Body = &body
	p.Origin = &api.DecisionOrigin{PaneID: "p1", Command: "command"}
	p.AllowText = true
	d, err := s.CreateDecision(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateDecision(p, time.Now())
	if err != nil || d.DecisionID == other.DecisionID {
		t.Fatalf("unique: %v", err)
	}
	if len(d.DecisionID) < 4 || d.DecisionID[:4] != "dec-" {
		t.Fatal(d.DecisionID)
	}
	got, err := s.Decision(d.DecisionID)
	if err != nil || !reflect.DeepEqual(d, got) {
		t.Fatalf("get %v %v", got, err)
	}
	ds, err := s.Decisions("pending")
	if err != nil || len(ds) != 2 {
		t.Fatalf("list %v %v", ds, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.Decision(d.DecisionID)
	if err != nil || !reflect.DeepEqual(d, got) {
		t.Fatalf("restart %v %v", got, err)
	}
}
func TestDecisionFirstAnswerAndValidation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := decisionParams()
	d, err := s.CreateDecision(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	invalid := "missing"
	text := "text"
	yes := "yes"
	no := "no"
	for _, a := range []api.DecisionAnswerParams{{DecisionID: d.DecisionID}, {DecisionID: d.DecisionID, OptionID: &invalid}, {DecisionID: d.DecisionID, Text: &text}} {
		if _, err := s.AnswerDecision(a, time.Now()); err == nil {
			t.Fatal("invalid answer accepted")
		}
	}
	var wg sync.WaitGroup
	out := make(chan api.Decision, 2)
	errs := make(chan error, 2)
	for _, option := range []*string{&yes, &no} {
		wg.Add(1)
		go func(o *string) {
			defer wg.Done()
			got, err := s.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: o, Responder: *o}, time.Now())
			if err != nil {
				errs <- err
			} else {
				out <- got
			}
		}(option)
	}
	wg.Wait()
	if len(out) != 1 || len(errs) != 1 {
		t.Fatalf("wins=%d errs=%d", len(out), len(errs))
	}
	winner := <-out
	if e, ok := (<-errs).(*api.Error); !ok || e.Code != api.ErrDecisionResolved {
		t.Fatalf("error=%v", e)
	}
	if _, err := s.CancelDecision(d.DecisionID, time.Now()); err == nil {
		t.Fatal("cancel resolved")
	}
	got, _ := s.Decision(d.DecisionID)
	if !reflect.DeepEqual(winner, got) {
		t.Fatal("answer overwritten")
	}
	for _, change := range []func(*api.DecisionCreateParams){func(p *api.DecisionCreateParams) { p.Title = "" }, func(p *api.DecisionCreateParams) { p.Kind = "bad" }, func(p *api.DecisionCreateParams) { p.Options = nil }, func(p *api.DecisionCreateParams) { p.Options[1].ID = "yes" }, func(p *api.DecisionCreateParams) { p.Options[0].Role = "bad" }} {
		p := decisionParams()
		change(&p)
		if _, err := s.CreateDecision(p, time.Now()); err == nil {
			t.Fatal("invalid create accepted")
		}
	}
}
func TestDecisionDeadlineAndText(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := decisionParams()
	ms := uint64(10)
	p.TimeoutMS = &ms
	p.AllowText = true
	now := time.Now()
	d, err := s.CreateDecision(p, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExpireDecision(d.DecisionID, now); err == nil {
		t.Fatal("early expiry")
	}
	expired, err := s.ExpireDecision(d.DecisionID, now.Add(10*time.Millisecond))
	if err != nil || expired.Status != "expired" {
		t.Fatalf("expiry %v %v", expired, err)
	}
	p.TimeoutMS = nil
	d, _ = s.CreateDecision(p, now)
	text := "a response"
	answered, err := s.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, Text: &text}, now)
	if err != nil || answered.Answer.Text == nil || *answered.Answer.Text != text {
		t.Fatalf("text %v %v", answered, err)
	}
	raw, _ := json.Marshal(answered)
	if !json.Valid(raw) {
		t.Fatal("record JSON")
	}
}
