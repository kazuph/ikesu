package dispatch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestScreenQuestionIdentitySurvivesRestartAndRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "質問", Options: []api.DecisionOption{{ID: "1", Label: "選択", Role: "other"}}, Origin: &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt"}}
	identity := `["session","tool",0]`
	d, err := store.CreateScreenDecision(params, time.Now(), "p1", identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, found, err := store.ScreenQuestionDecision("p1", identity)
	if err != nil || !found || got.DecisionID != d.DecisionID {
		t.Fatalf("restored: %+v %v %v", got, found, err)
	}
	if err := store.BindScreenQuestion(d.DecisionID, "p1", identity); err != nil {
		t.Fatal("idempotent binding", err)
	}
	if err := store.BindScreenQuestion(d.DecisionID, "p1", `["session","different",0]`); err == nil {
		t.Fatal("decision reassigned to another question")
	}
	if _, err := store.CreateScreenDecision(params, time.Now(), "p1", identity); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	all, err := store.Decisions("pending")
	if err != nil || len(all) != 1 {
		t.Fatalf("duplicate insert was not rolled back: %+v %v", all, err)
	}
	other, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BindScreenQuestion(other.DecisionID, "p1", identity); err == nil {
		t.Fatal("duplicate binding accepted")
	}
	if _, err := store.CancelDecision(d.DecisionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.ScreenQuestionDecision("p1", identity); err != nil || found {
		t.Fatalf("resolved identity: %v %v", found, err)
	}
	recreated, err := store.CreateScreenDecision(params, time.Now(), "p1", identity)
	if err != nil {
		t.Fatalf("locally cancelled question did not return: %v", err)
	}
	if _, err := store.AnswerDecision(api.DecisionAnswerParams{DecisionID: recreated.DecisionID, OptionIDs: []string{"1"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, found, err := store.ScreenQuestionDecisionAny("p1", identity); err != nil || !found || got.DecisionID != recreated.DecisionID || got.Status != "answered" {
		t.Fatalf("answered identity lookup: %+v %v %v", got, found, err)
	}
	if _, err := store.CreateScreenDecision(params, time.Now(), "p1", identity); err == nil {
		t.Fatal("answered question recreated")
	}
}

func TestBindScreenQuestionAfterLocalCancellation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "質問", Options: []api.DecisionOption{{ID: "1", Label: "選択", Role: "other"}}, Origin: &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt"}}
	identity := `["session","tool",0]`
	old, err := store.CreateScreenDecision(params, time.Now(), "p1", identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelDecision(old.DecisionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	fallback, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BindScreenQuestion(fallback.DecisionID, "p1", identity); err != nil {
		t.Fatalf("delayed transcript failed to bind: %v", err)
	}
	if _, err = store.AnswerDecision(api.DecisionAnswerParams{DecisionID: fallback.DecisionID, OptionIDs: []string{"1"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BindScreenQuestion(other.DecisionID, "p1", identity); err == nil {
		t.Fatal("answered question rebound")
	}
}

func TestClaudeScreenContextSurvivesRestartAndRejectsNewerProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "質問", Options: []api.DecisionOption{{ID: "1", Label: "選択", Role: "other"}}, Origin: &api.DecisionOrigin{PaneID: "p1", Agent: "claude", Reason: "screen_prompt"}}
	d, err := store.CreateDecision(params, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BindClaudeScreenContext(d.DecisionID, "p1", "session", now.Add(time.Second)); err == nil {
		t.Fatal("newer process accepted")
	}
	start := now.Add(-time.Second)
	if err := store.BindClaudeScreenContext(d.DecisionID, "p1", "session", start); err != nil {
		t.Fatal(err)
	}
	if err := store.BindClaudeScreenContext(d.DecisionID, "p1", "other-session", start); err == nil {
		t.Fatal("context was reassigned to another session")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pane, id, got, found, err := store.ClaudeScreenContext(d.DecisionID)
	if err != nil || !found || pane != "p1" || id != "session" || !got.Equal(start) {
		t.Fatalf("restored context: %q %q %v %v %v", pane, id, got, found, err)
	}
}
