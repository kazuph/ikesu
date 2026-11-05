package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestCodexQuestionMatchesUniquePendingCall(t *testing.T) {
	p := screenprompt.Parse("codex", strings.Split(screenDialog(t, "codex-question"), "\n"))
	q := session.CodexQuestion{CallID: "first", ID: "format", Index: 0, Total: 2, Question: "Report format?", State: session.CodexPending, Options: []session.CodexQuestionOption{{Label: "Plain text", Description: "Use plain text."}, {Label: "JSON", Description: "Use JSON."}}}
	if got, ok := chooseCodexQuestion(p, []session.CodexQuestion{q}); !ok || got.CallID != "first" {
		t.Fatalf("match: %+v %v", got, ok)
	}
	second := q
	second.CallID = "second"
	if _, ok := chooseCodexQuestion(p, []session.CodexQuestion{q, second}); ok {
		t.Fatal("ambiguous calls matched")
	}
	q.State = session.CodexEmpty
	if got, ok := chooseCodexQuestion(p, []session.CodexQuestion{q, second}); !ok || got.CallID != "second" {
		t.Fatalf("repeated question: %+v %v", got, ok)
	}
	result := codexQuestionScan(codexBinding{question: q}, p, []session.CodexQuestion{q, second})
	if result.state != session.CodexEmpty || !result.nextCall {
		t.Fatalf("same-screen repeated call: %+v", result)
	}
	second.Index = 1
	if _, ok := chooseCodexQuestion(p, []session.CodexQuestion{second}); ok {
		t.Fatal("wrong question index matched")
	}
}

func TestLateAnswerRemainsAnsweredWhileDeliveryIsPending(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-answer")
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d, err := store.CreateDecision(api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", AllowText: true, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.screenPrompts.late = map[string]*lateCodexQuestion{d.DecisionID: {}}
	id, note := "1", "濃い青"
	params := api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &id, Text: &note, Responder: "test"}
	req := &api.Request{ID: "answer", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(params)}
	resp := s.decisionRequest(req)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	var result api.DecisionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision.Status != "answered" || result.Decision.Delivery == nil || result.Decision.Delivery.Status != "pending" {
		t.Fatal(result)
	}
	if again := s.decisionRequest(req); again.Err == nil || again.Err.Code != api.ErrDecisionResolved {
		t.Fatal(again)
	}
	stored, err := store.Decision(d.DecisionID)
	if err != nil || stored.Delivery != nil {
		t.Fatalf("delivery was persisted: %+v %v", stored, err)
	}
}

func TestLateAnswerSimultaneousClientsAcceptOnlyOne(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-race")
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d, err := store.CreateDecision(api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}, {ID: "2", Label: "赤", Role: "other"}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.screenPrompts.late = map[string]*lateCodexQuestion{d.DecisionID: {}}
	done := make(chan struct{})
	s.app.SetQuitHook(s.stopDecisions)
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done; s.sup.KillAll() }()
	responses := make(chan api.Response, 2)
	start := make(chan struct{})
	for _, choice := range []string{"1", "2"} {
		choice := choice
		go func() {
			<-start
			var response api.Response
			s.app.Do(func() {
				response = s.decisionRequest(&api.Request{ID: "answer", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: d.DecisionID, OptionID: &choice})})
			})
			responses <- response
		}()
	}
	close(start)
	a, b := <-responses, <-responses
	if (a.Err == nil) == (b.Err == nil) {
		t.Fatalf("responses: %+v %+v", a, b)
	}
	failed := a
	if failed.Err == nil {
		failed = b
	}
	if failed.Err.Code != api.ErrDecisionResolved {
		t.Fatal(failed.Err)
	}
	stored, err := store.Decision(d.DecisionID)
	if err != nil || stored.Status != "answered" {
		t.Fatal(stored, err)
	}
}

func TestLateDeliveryIsNotCopiedIntoEventJournal(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-journal")
	d := api.Decision{DecisionID: "dec-test", Status: "answered", Delivery: &api.DecisionDelivery{Status: "pending", Text: "回答文"}}
	s.emitEvent("decision_resolved", d)
	if len(s.journal) != 1 {
		t.Fatal(s.journal)
	}
	var recorded api.Decision
	if err := json.Unmarshal(s.journal[0].Data, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.Delivery != nil {
		t.Fatal("delivery copied into handoff journal", recorded.Delivery)
	}
}

func TestLateDeliveryClosedPaneFailsWithoutSending(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-closed")
	late := &lateCodexQuestion{pid: 999, delivery: &api.DecisionDelivery{Status: "pending", Text: "回答文"}}
	s.screenPrompts.late = map[string]*lateCodexQuestion{"dec-closed": late}
	s.scanLateCodexDeliveries()
	if late.sent || late.delivery.Status != "failed" || late.delivery.Reason != "ペインが閉じました" {
		t.Fatal(late)
	}
}

func TestEmptyQuestionsFromSameCallGetIndependentDecisions(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-group")
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	origin := &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt", Agent: "codex"}
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", AllowText: true, Origin: origin, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}
	identity := `["session","call","color"]`
	d, err := store.CreateScreenDecision(params, time.Now(), "p1", identity)
	if err != nil {
		t.Fatal(err)
	}
	w := &screenPromptWatch{decisionID: d.DecisionID, codex: &codexBinding{path: "/tmp/rollout", identity: identity, question: session.CodexQuestion{SessionID: "session", CallID: "call", ID: "color"}}}
	questions := []session.CodexQuestion{
		{SessionID: "session", CallID: "call", ID: "color", Index: 0, Total: 3, Question: "色は？", State: session.CodexEmpty, Options: []session.CodexQuestionOption{{Label: "青"}}},
		{SessionID: "session", CallID: "call", ID: "shape", Index: 1, Total: 3, Question: "形は？", State: session.CodexEmpty, Options: []session.CodexQuestionOption{{Label: "丸"}}},
		{SessionID: "session", CallID: "call", ID: "size", Index: 2, Total: 3, Question: "大きさは？", State: session.CodexAnswered, Options: []session.CodexQuestionOption{{Label: "大"}}},
	}
	s.retainOtherEmptyCodexQuestions(1, w, questions)
	s.retainOtherEmptyCodexQuestions(1, w, questions)
	pending, err := store.Decisions("pending")
	if err != nil || len(pending) != 2 {
		t.Fatalf("decisions: %+v %v", pending, err)
	}
	var shape api.Decision
	for _, item := range pending {
		if item.Title == "形は？" {
			shape = item
		}
	}
	if shape.DecisionID == "" || s.screenPrompts.late[shape.DecisionID] == nil {
		t.Fatal("empty question not retained", pending)
	}
}

func TestLateQuestionReplacesExpiredCountdown(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-countdown")
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	body := "auto-resolves in s"
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", Body: &body, Origin: &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt"}, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}
	d, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	w := &screenPromptWatch{decisionID: d.DecisionID, prompt: &screenprompt.Prompt{Params: params}}
	if !s.retainLateCodexQuestion(1, w) {
		t.Fatal("late question was not retained")
	}
	got, err := store.Decision(d.DecisionID)
	if err != nil || got.Body == nil || *got.Body != "Codex は空の答えで先に進みました。答えると会話で届けます" {
		t.Fatal(got, err)
	}
}

func TestLateDeliveryIgnoresSiblingLateQuestionsButWaitsForOtherDecision(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-blocker")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	params := api.DecisionCreateParams{Kind: "agent_prompt", Title: "質問", Origin: &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt"}, Options: []api.DecisionOption{{ID: "1", Label: "はい", Role: "other"}}}
	first, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDecision(params, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.screenPrompts.late = map[string]*lateCodexQuestion{first.DecisionID: {}}
	if !s.lateDeliveryHasOtherDecision(1) {
		t.Fatal("active confirmation was ignored")
	}
	s.screenPrompts.late[second.DecisionID] = &lateCodexQuestion{}
	if s.lateDeliveryHasOtherDecision(1) {
		t.Fatal("other late questions blocked delivery")
	}
}

func TestLateCodexAnswerTextPreservesSelectionAndNote(t *testing.T) {
	choice, note := "1", "濃い青"
	d := api.Decision{Title: "色は？", Options: []api.DecisionOption{{ID: "1", Label: "青"}}}
	got, err := lateCodexAnswerText(d, api.DecisionAnswer{OptionID: &choice, Text: &note})
	if err != nil || got != "さっきの質問『色は？』への回答：青（メモ：濃い青）" {
		t.Fatal(got, err)
	}
	got, err = lateCodexAnswerText(d, api.DecisionAnswer{Text: &note})
	if err != nil || got != "さっきの質問『色は？』への回答：濃い青" {
		t.Fatal(got, err)
	}
}

func TestCodexBoundQuestionDoesNotFollowLaterCall(t *testing.T) {
	bound := session.CodexQuestion{SessionID: "session", CallID: "old", ID: "color"}
	questions := []session.CodexQuestion{
		{SessionID: "session", CallID: "old", ID: "color", State: session.CodexEmpty},
		{SessionID: "session", CallID: "new", ID: "color", State: session.CodexPending},
	}
	if got := boundCodexState(bound, questions); got != session.CodexEmpty {
		t.Fatal(got)
	}
	questions = append(questions, questions[0])
	if got := boundCodexState(bound, questions); got != "" {
		t.Fatalf("duplicate accepted: %s", got)
	}
}
