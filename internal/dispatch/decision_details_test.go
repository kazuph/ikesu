package dispatch

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestDecisionDetailsOldPayloadAndPersistence(t *testing.T) {
	var old api.Decision
	if err := json.Unmarshal([]byte(`{"decision_id":"dec-old","options":[{"id":"1","label":"Old","role":"other"}],"answer":{"option_id":"1"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.MultiSelect || old.QuestionLabel != nil || old.Options[0].Description != nil || old.Answer.OptionIDs != nil {
		t.Fatal(old)
	}
	raw, _ := json.Marshal(old)
	for _, field := range []string{"description", "preview", "question_label", "option_ids"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("unexpected %s: %s", field, raw)
		}
	}
	path := filepath.Join(t.TempDir(), "details.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p := decisionParams()
	p.MultiSelect = true
	detail, preview, label := "two lines\ndetail", "draft\nbody", "質問 1/2 Format"
	p.Options[0].Description = &detail
	p.Options[0].Preview = &preview
	p.Options[1].PreviewUnavailable = true
	p.QuestionLabel = &label
	d, err := s.CreateDecision(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Decision(d.DecisionID)
	if err != nil || !reflect.DeepEqual(d, got) {
		t.Fatalf("%+v %v", got, err)
	}
	answered, err := s.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionIDs: []string{"yes", "no", "yes"}}, time.Now())
	if err != nil || !reflect.DeepEqual(answered.Answer.OptionIDs, []string{"yes", "no"}) || *answered.Answer.OptionID != "yes" {
		t.Fatalf("%+v %v", answered, err)
	}
	if _, err := s.AnswerDecision(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionIDs: []string{"no"}}, time.Now()); err == nil {
		t.Fatal("second answer accepted")
	}
}

func TestDecisionMultipleAnswerValidation(t *testing.T) {
	d := api.Decision{Options: decisionParams().Options, MultiSelect: true}
	yes := "yes"
	for _, p := range []api.DecisionAnswerParams{{OptionIDs: []string{"missing"}}, {OptionIDs: []string{}}, {OptionID: &yes, Text: &yes}} {
		if _, err := ValidateDecisionAnswer(d, p); err == nil {
			t.Fatalf("invalid accepted: %+v", p)
		}
	}
	for _, p := range []api.DecisionAnswerParams{{OptionID: &yes}, {OptionIDs: []string{"yes", "yes"}}, {OptionIDs: []string{"yes", "no"}, OptionID: &yes}} {
		a, err := ValidateDecisionAnswer(d, p)
		if err != nil || a.OptionID == nil || *a.OptionID != "yes" {
			t.Fatalf("%+v %v", a, err)
		}
	}
}
