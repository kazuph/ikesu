package server

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestLateDeliveryWaitsForReceiptOfPriorAnswerInSamePane(t *testing.T) {
	s := &Server{}
	s.screenPrompts.late = map[string]*lateCodexQuestion{
		"a": {pid: 1, sent: true, delivery: &api.DecisionDelivery{Status: "pending"}},
		"b": {pid: 1, delivery: &api.DecisionDelivery{Status: "pending"}},
		"c": {pid: 2, delivery: &api.DecisionDelivery{Status: "pending"}},
	}
	if !s.lateDeliveryInFlight(1, "b") {
		t.Fatal("second answer could overtake first")
	}
	if s.lateDeliveryInFlight(2, "c") {
		t.Fatal("unrelated pane blocked")
	}
	s.screenPrompts.late["a"].delivery.Status = "delivered"
	if s.lateDeliveryInFlight(1, "b") {
		t.Fatal("delivered answer still blocks")
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

func TestRestoredCodexMatchRequiresUniqueEmptyResult(t *testing.T) {
	label := "Question 1/1"
	d := api.Decision{Title: "色は？", QuestionLabel: &label, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}
	q := session.CodexQuestion{SessionID: "s", CallID: "c", ID: "q", Index: 0, Total: 1, Question: "色は？", Options: []session.CodexQuestionOption{{Label: "青"}}, State: session.CodexEmpty}
	if _, ok := restoredCodexMatch(d, []session.CodexQuestion{q}, `["s","c","q"]`); !ok {
		t.Fatal("recorded empty question not restored")
	}
	for _, state := range []session.CodexAnswerState{session.CodexPending, session.CodexAnswered} {
		other := q
		other.State = state
		if _, ok := restoredCodexMatch(d, []session.CodexQuestion{other}, ""); ok {
			t.Fatalf("accepted %s", state)
		}
	}
	other := q
	other.CallID = "another"
	if _, ok := restoredCodexMatch(d, []session.CodexQuestion{q, other}, ""); ok {
		t.Fatal("ambiguous calls accepted")
	}
	if got, ok := restoredCodexMatch(d, []session.CodexQuestion{q, other}, `["s","c","q"]`); !ok || got.CallID != "c" {
		t.Fatal("recorded identity did not disambiguate")
	}
	if _, ok := restoredCodexMatch(d, []session.CodexQuestion{q}, `["s","other","q"]`); ok {
		t.Fatal("foreign identity accepted")
	}
}

func TestCodexRetryBindsOriginalQuestionAfterIncompleteRecord(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "late-retry-record")
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	raw := `{"type":"session_meta","payload":{"id":"s"}}` + "\n" +
		`{"timestamp":"` + now.Add(-time.Second).Format(time.RFC3339Nano) + `","type":"response_item","payload":{"type":"function_call","name":"request_user_input","call_id":"original","arguments":"{\"questions\":[{\"id\":\"q\",\"question\":\"色は？\",\"options\":[{\"label\":\"青\"}]}]}"}}` + "\n"
	if err := os.WriteFile(path, []byte(raw+"{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ReadCodexQuestions(path, "s"); err == nil {
		t.Fatal("incomplete record accepted")
	}
	ch := make(chan codexBindingResult, 1)
	ch <- codexBindingResult{path: path, sessionID: "s", start: now.Add(-time.Minute), group: 1}
	label := "Question 1/1"
	prompt := &screenprompt.Prompt{CodexQuestion: true, Params: api.DecisionCreateParams{Title: "色は？", QuestionLabel: &label, Options: []api.DecisionOption{{ID: "1", Label: "青"}}}}
	w := &screenPromptWatch{prompt: prompt, codexResult: ch}
	s.waitCodexBinding(1, w)
	if w.codexChecked || w.codexAnchorSession != "s" {
		t.Fatalf("retry anchor %+v", w)
	}
	d, err := store.CreateDecision(api.DecisionCreateParams{Kind: "agent_prompt", Title: "色は？", Origin: &api.DecisionOrigin{PaneID: fmtPaneID(1)}, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	w.decisionID = d.DecisionID
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	qs, err := session.ReadCodexQuestions(path, "s")
	if err != nil || len(qs) != 1 {
		t.Fatalf("questions=%+v err=%v", qs, err)
	}
	identity := `["s","original","q"]`
	newer := qs[0]
	newer.CallID = "newer"
	newer.Timestamp = now.Add(time.Second)
	for _, bad := range []codexBindingResult{
		{path: path, sessionID: "foreign", start: now.Add(-time.Minute), group: 1, binding: &codexBinding{path: path, question: qs[0], identity: identity}},
		{path: path, sessionID: "s", start: now.Add(-time.Minute), group: 1, binding: &codexBinding{path: path, question: newer, identity: `["s","newer","q"]`}},
	} {
		wrong := make(chan codexBindingResult, 1)
		wrong <- bad
		w.codexResult = wrong
		s.waitCodexBinding(1, w)
		if w.codexChecked || w.codex != nil {
			t.Fatalf("incorrect call bound: %+v", bad)
		}
	}
	ch2 := make(chan codexBindingResult, 1)
	ch2 <- codexBindingResult{path: path, sessionID: "s", start: now.Add(-time.Minute), group: 1, binding: &codexBinding{path: path, question: qs[0], identity: identity}}
	w.codexResult = ch2
	s.waitCodexBinding(1, w)
	if !w.codexChecked || w.codex == nil || w.codex.question.CallID != "original" {
		t.Fatalf("binding=%+v", w.codex)
	}
	saved, err := store.ScreenQuestionIdentity(d.DecisionID)
	if err != nil || saved != identity {
		t.Fatalf("identity=%q err=%v", saved, err)
	}
}

func TestLegacyRestoreRejectsProcessStartedAfterDecision(t *testing.T) {
	created := time.Date(2026, 11, 6, 2, 0, 30, 0, time.UTC)
	label := "Question 1/1"
	d := api.Decision{Title: "色は？", CreatedUnixMS: created.UnixMilli(), QuestionLabel: &label, Options: []api.DecisionOption{{ID: "1", Label: "青", Role: "other"}}}
	q := session.CodexQuestion{Timestamp: created.Add(-time.Second), SessionID: "resumed-other", CallID: "call", ID: "q", Index: 0, Total: 1, Question: "色は？", Options: []session.CodexQuestionOption{{Label: "青"}}, State: session.CodexEmpty}
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q}, "", created.Add(time.Second)); ok {
		t.Fatal("resumed session bound to a process started after the decision")
	}
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q}, "", created); ok {
		t.Fatal("process with an equal start time was accepted")
	}
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q}, "", time.Time{}); ok {
		t.Fatal("unknown process start was accepted")
	}
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q}, `["resumed-other","call","q"]`, created.Add(time.Second)); !ok {
		t.Fatal("recorded question identity should retain existing behavior")
	}
	q.SessionID = "original"
	if _, ok := restoredCodexMatchAtProcessStart(d, []session.CodexQuestion{q}, "", created.Add(-time.Minute)); !ok {
		t.Fatal("original session did not restore")
	}

	teststate.Lock(t)
	s, _, input := screenTerminal(t, "codex-question", "codex")
	store, err := s.db()
	if err != nil {
		t.Fatal(err)
	}
	decision, err := store.CreateDecision(api.DecisionCreateParams{Kind: "agent_prompt", Title: d.Title, QuestionLabel: &label, Options: d.Options, Origin: &api.DecisionOrigin{PaneID: "p1", Agent: "codex", Reason: "screen_prompt"}}, created)
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	choice := "1"
	s.app.Do(func() {
		response = s.decisionRequest(&api.Request{ID: "answer", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(api.DecisionAnswerParams{DecisionID: decision.DecisionID, OptionID: &choice})})
	})
	if response.Err == nil || response.Err.Code != api.ErrDecisionStale {
		t.Fatalf("unbound decision accepted: %+v", response.Err)
	}
	if keys, err := os.ReadFile(input); err != nil || len(keys) != 0 {
		t.Fatalf("input reached terminal: %q, %v", keys, err)
	}
}
