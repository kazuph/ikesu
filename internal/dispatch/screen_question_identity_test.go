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
	if _, err := store.CreateScreenDecision(params, time.Now(), "p1", identity); err == nil {
		t.Fatal("answered question recreated")
	}
}
