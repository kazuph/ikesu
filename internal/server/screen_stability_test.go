package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/screenprompt"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"strings"
	"testing"
	"time"
)

func TestScreenExternalInputFreezesDecisionAndRejectsAnswer(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	p.RecordExternalInput()
	paintScreen(p, "ordinary idle screen")
	scanScreen(s)
	scanScreen(s)
	resp := answerScreen(s, d, "1", nil)
	if resp.Err == nil || resp.Err.Code != "pane_user_active" {
		t.Fatal(resp)
	}
	got, err := s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatal(got, err)
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal(keys)
	}
	time.Sleep(3 * time.Second)
	scanScreen(s)
	scanScreen(s)
	got, err = s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatal(got, err)
	}
}

func TestScreenExternalInputPreventsCreateAndPreviewRestoration(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-preview", "claude")
	p.RecordExternalInput()
	scanScreen(s)
	if s.store != nil {
		all, _ := s.store.Decisions("")
		if len(all) != 0 {
			t.Fatal(all)
		}
	}
	time.Sleep(3 * time.Second)
	scanScreen(s)
	d := screenRecord(t, s)
	if d.Status != "pending" {
		t.Fatal(d)
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal("deferred scan sent keys", keys)
	}
	s.app.Do(func() {
		w := s.screenPrompts.watches[1]
		w.preview = &screenPreviewScan{original: 1, visited: map[string]*string{}, lastDirection: "\x1b[B"}
		p.RecordExternalInput()
		s.restorePreviewCursor(1, w)
		s.scanScreenPrompts()
		if w.preview != nil {
			t.Error("scan did not abort")
		}
	})
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal("restore sent keys", keys)
	}
}

func TestScreenVolatileUpdateAndRebuildFlood(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
	picture := screenDialog(t, "claude-single")
	paintScreen(p, strings.ReplaceAll(picture, "A compact plain text report", "A compact plain text report (12s) ⠋"))
	scanScreen(s)
	original := screenRecord(t, s)
	events := len(s.journal)
	paintScreen(p, strings.ReplaceAll(picture, "A compact plain text report", "A compact plain text report (13s) ⠙"))
	scanScreen(s)
	changed := screenRecord(t, s)
	if changed.DecisionID != original.DecisionID || changed.CreatedUnixMS != original.CreatedUnixMS || changed.Options[0].Description == nil || !strings.Contains(*changed.Options[0].Description, "13s") || len(s.journal) != events {
		t.Fatal(changed)
	}
	for i := 0; i < 4; i++ {
		previous := screenRecord(t, s)
		paintScreen(p, strings.ReplaceAll(picture, "Which output format", strings.Repeat("Different ", i+1)+"output format"))
		scanScreen(s)
		scanScreen(s)
		next := screenRecord(t, s)
		if i < 3 && next.DecisionID == previous.DecisionID {
			t.Fatal("normal transition suppressed")
		}
		if i == 3 && next.DecisionID != previous.DecisionID {
			t.Fatal("fourth reconstruction flooded")
		}
	}
}

func TestScreenFinalSubmitRequiresAllAnswersAndNoExternalInput(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-final-confirm", "claude")
	scanScreen(s)
	if len(s.screenPrompts.watches) != 0 {
		t.Fatal("final became a decision")
	}
	final := screenprompt.Parse("claude", screenpromptLines(t, "claude-final-confirm"))
	s.app.Do(func() {
		s.screenPrompts.flows = map[int]*screenAnswerFlow{1: {name: final.Flow, total: 2, answered: map[int]bool{0: true}}}
		s.submitScreenFlow(1, final)
	})
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal("partial flow submitted", keys)
	}
	s.app.Do(func() {
		s.screenPrompts.flows[1].answered[1] = true
		s.submitScreenFlow(1, final)
		s.submitScreenFlow(1, final)
	})
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\r" })
	s.app.Do(func() {
		s.screenPrompts.flows[1].submitted = false
		p.RecordExternalInput()
		s.submitScreenFlow(1, final)
	})
	if keys, _ := os.ReadFile(input); string(keys) != "\r" {
		t.Fatal("interrupted flow submitted", keys)
	}
}
func screenpromptLines(t *testing.T, name string) []string {
	return strings.Split(screenDialog(t, name), "\n")
}

func TestScreenAnswerInterruptionDiscardsDelayedKeys(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	text := "delayed"
	responses := make(chan api.Response, 1)
	go func() { responses <- answerScreen(s, d, "", &text) }()
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "3" })
	p.RecordExternalInput()
	resp := <-responses
	if resp.Err == nil || resp.Err.Code != "screen_prompt_input_failed" {
		t.Fatal("interruption accepted", resp)
	}
	if keys, _ := os.ReadFile(input); string(keys) != "3" {
		t.Fatal(keys)
	}
	got, _ := s.store.Decision(d.DecisionID)
	if got.Status != "answered" {
		t.Fatal(got)
	}
}

func TestScreenPaneAPIInputRecordsAtAdmission(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	requests := []api.Request{
		{Method: api.MethodPaneSendText, Params: api.MarshalParams(api.PaneSendTextParams{PaneID: "p1", Text: "x"})},
		{Method: api.MethodPaneSendKeys, Params: api.MarshalParams(api.PaneSendKeysParams{PaneID: "p1", Keys: []string{"Right"}})},
		{Method: api.MethodPaneSendInput, Params: api.MarshalParams(api.PaneSendInputParams{PaneID: "p1", Text: "y"})},
		{Method: api.MethodAgentSend, Params: api.MarshalParams(api.AgentSendParams{Target: "p1", Text: "z"})},
	}
	for _, req := range requests {
		previous := p.ExternalInputSequence()
		resp := s.dispatch(&req)
		if resp.Err != nil {
			t.Fatal(resp.Err)
		}
		if !p.UserActive() || p.ExternalInputSequence() <= previous {
			t.Fatal("input was not recorded at admission", req.Method)
		}
		if resp := answerScreen(s, d, "1", nil); resp.Err == nil || resp.Err.Code != "pane_user_active" {
			t.Fatal(req.Method, resp)
		}
	}
}

func TestScreenPermissionTallerThanPaneRetainsHeaderAndCommand(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-permission", "claude")
	s.sup.Resize(1, 160, 12)
	s.app.Do(func() {
		prompt := s.readScreenPrompt(1)
		if prompt == nil || !strings.Contains(prompt.Params.Title, "Bash command") || !strings.Contains(prompt.Params.Title, "/tmp/gs-j0m1z8np/work/permission-observation.txt") {
			t.Error("tall permission lost context", prompt, p.DetectionLines())
		}
	})
}
