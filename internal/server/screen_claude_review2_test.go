package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/teststate"
)

type review2Fixture struct {
	s                        *Server
	pane                     *runtime.Pane
	root, id, input, picture string
}

func newReview2Fixture(t *testing.T) *review2Fixture {
	t.Helper()
	s := newTestServer(t, "claude-review2")
	root, cwd := t.TempDir(), t.TempDir()
	id := "abababab-abab-abab-abab-abababababab"
	if err := os.MkdirAll(filepath.Join(root, "projects", "cwd"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "claude")
	if err = os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	picture := screenDialog(t, "claude-single")
	screen := filepath.Join(root, "screen")
	if err = os.WriteFile(screen, []byte(picture), 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "input")
	pane, err := s.sup.SpawnWith(1, cwd, 160, 60, []string{"CLAUDE_PTY_EMULATOR=1", "CLAUDE_CONFIG_DIR=" + root, "EMULATOR_SESSION=" + id, "EMULATOR_SCREEN=" + screen, "EMULATOR_INPUT=" + input}, []string{link, "-test.run=^TestClaudeTranscriptPTYEmulator$"})
	if err != nil {
		t.Fatal(err)
	}
	pane.SetAgent("claude")
	s.app.SetReported(1, "claude", "blocked")
	s.initDecisionHub()
	s.app.SetQuitHook(s.stopDecisions)
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	t.Cleanup(func() {
		s.app.Stop()
		<-done
		s.sup.KillAll()
		if s.store != nil {
			s.store.Close()
		}
	})
	waitPromptCondition(t, func() bool {
		return strings.Contains(pane.ScreenText(), "Enter to select") && foregroundAgent(pane) == "claude"
	})
	return &review2Fixture{s, pane, root, id, input, picture}
}
func (f *review2Fixture) create(t *testing.T, identity string) api.Decision {
	t.Helper()
	var d api.Decision
	f.s.app.Do(func() {
		p := f.s.readScreenPrompt(1)
		w := &screenPromptWatch{prompt: p, signature: p.Signature(), lastMatched: true, transcriptID: identity}
		f.s.screenPrompts.watches = map[int]*screenPromptWatch{1: w}
		f.s.createScreenPrompt(1, w)
		var err error
		d, err = f.s.store.Decision(w.decisionID)
		if err != nil {
			t.Error(err)
		}
	})
	return d
}
func (f *review2Fixture) transcript(t *testing.T, records ...any) {
	t.Helper()
	var data []byte
	for _, r := range records {
		b, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		data = append(append(data, b...), '\n')
	}
	if e := os.WriteFile(filepath.Join(f.root, "projects", "cwd", f.id+".jsonl"), data, 0600); e != nil {
		t.Fatal(e)
	}
}
func (f *review2Fixture) question(d api.Decision, tool string, at time.Time, title string) any {
	options := []map[string]string{}
	for _, o := range d.Options {
		desc := ""
		if o.Description != nil {
			desc = *o.Description
		}
		options = append(options, map[string]string{"label": o.Label, "description": desc})
	}
	return map[string]any{"timestamp": at.UTC().Format(time.RFC3339Nano), "sessionId": f.id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "name": "AskUserQuestion", "id": tool, "input": map[string]any{"questions": []any{map[string]any{"question": title, "options": options}}}}}}}
}
func (f *review2Fixture) result(tool string, at time.Time) any {
	return map[string]any{"timestamp": at.UTC().Format(time.RFC3339Nano), "sessionId": f.id, "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": tool}}}}
}
func review2After(t *testing.T, ms int64) time.Time {
	t.Helper()
	waitPromptCondition(t, func() bool { return time.Now().UnixMilli() > ms })
	return time.Now()
}
func TestClaudeSameTextNextQuestionCannotBindOldWatch(t *testing.T) {
	teststate.Lock(t)
	for _, mode := range []string{"scan", "answer"} {
		t.Run(mode, func(t *testing.T) {
			f := newReview2Fixture(t)
			before := time.Now()
			review2After(t, before.UnixMilli())
			old := f.create(t, "")
			waitPromptCondition(t, func() bool { _, _, _, ok, _ := f.s.store.ClaudeScreenContext(old.DecisionID); return ok })
			f.s.app.Do(func() { f.s.screenPrompts.claudeRestore = nil })
			after := review2After(t, old.CreatedUnixMS)
			f.transcript(t, f.question(old, "old", before, old.Title), f.result("old", after), f.question(old, "next", after, old.Title))
			if mode == "scan" {
				waitPromptCondition(t, func() bool {
					scanScreen(f.s)
					d, _ := f.s.store.Decision(old.DecisionID)
					identity, _ := f.s.store.ScreenQuestionIdentity(old.DecisionID)
					return d.Status != "pending" || identity != ""
				})
			} else {
				if r := answerScreen(f.s, old, "1", nil); r.Err == nil {
					t.Error("old decision accepted for next question")
				}
			}
			identity, err := f.s.store.ScreenQuestionIdentity(old.DecisionID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(identity, "next") {
				t.Errorf("old decision bound to next question: %s", identity)
			}
			data, _ := os.ReadFile(f.input)
			if len(data) != 0 {
				t.Errorf("input delivered to next question: %q", data)
			}
		})
	}
}
func TestClaudeAnsweredOwnerClosesIdenticalUnboundPending(t *testing.T) {
	teststate.Lock(t)
	f := newReview2Fixture(t)
	before := time.Now()
	review2After(t, before.UnixMilli())
	old := f.create(t, "")
	waitPromptCondition(t, func() bool { _, _, _, ok, _ := f.s.store.ClaudeScreenContext(old.DecisionID); return ok })
	identity := `["` + f.id + `","same",0]`
	var owner api.Decision
	f.s.app.Do(func() {
		p := f.s.readScreenPrompt(1).Params
		p.Origin = &api.DecisionOrigin{PaneID: "p1", Agent: "claude", Reason: "screen_prompt", Machine: "local"}
		var err error
		owner, err = f.s.store.CreateScreenDecision(p, time.Now(), "p1", identity)
		if err != nil {
			t.Error(err)
		}
		option := owner.Options[0].ID
		_, err = f.s.store.AnswerDecision(api.DecisionAnswerParams{DecisionID: owner.DecisionID, OptionID: &option}, time.Now())
		if err != nil {
			t.Error(err)
		}
		f.s.screenPrompts.watches = nil
	})
	after := review2After(t, old.CreatedUnixMS)
	f.transcript(t, f.question(old, "same", before, old.Title), f.result("same", after))
	paintScreen(f.pane, "ordinary idle screen")
	waitPromptCondition(t, func() bool {
		scanScreen(f.s)
		d, _ := f.s.store.Decision(old.DecisionID)
		return d.Status == "cancelled"
	})
}
func TestClaudeIdentifiedOldQuestionClosesOnNextQuestion(t *testing.T) {
	teststate.Lock(t)
	for _, result := range []string{"answered", "next_without_result", "late_identity"} {
		t.Run(result, func(t *testing.T) {
			f := newReview2Fixture(t)
			before := time.Now()
			review2After(t, before.UnixMilli())
			identity := `["` + f.id + `","old",0]`
			var old api.Decision
			if result == "late_identity" {
				old = f.create(t, "")
				f.s.app.Do(func() {
					if err := f.s.store.BindScreenQuestion(old.DecisionID, "p1", identity); err != nil {
						t.Error(err)
					}
					f.s.screenPrompts.watches[1].transcriptID = identity
				})
			} else {
				old = f.create(t, identity)
			}
			after := review2After(t, old.CreatedUnixMS)
			nextTitle := "A completely different question?"
			records := []any{f.question(old, "old", before, old.Title)}
			if result == "answered" {
				records = append(records, f.result("old", after))
			}
			records = append(records, f.question(old, "next", after, nextTitle))
			f.transcript(t, records...)
			paintScreen(f.pane, strings.ReplaceAll(f.picture, old.Title, nextTitle))
			waitPromptCondition(t, func() bool {
				scanScreen(f.s)
				d, _ := f.s.store.Decision(old.DecisionID)
				return d.Status == "cancelled"
			})
		})
	}
}

func TestClaudeDifferentSessionOrProcessCannotBindOldDecision(t *testing.T) {
	teststate.Lock(t)
	for _, mismatch := range []string{"session", "process"} {
		t.Run(mismatch, func(t *testing.T) {
			f := newReview2Fixture(t)
			start, ok := session.ProcessStart(f.pane.ForegroundJob())
			if !ok {
				t.Fatal("missing process start")
			}
			var old api.Decision
			f.s.app.Do(func() {
				if _, err := f.s.db(); err != nil {
					t.Error(err)
					return
				}
				p := f.s.readScreenPrompt(1)
				params := p.Params
				params.Origin = &api.DecisionOrigin{PaneID: "p1", Agent: "claude", Reason: "screen_prompt", Machine: "local"}
				var err error
				old, err = f.s.store.CreateDecision(params, time.Now())
				if err != nil {
					t.Error(err)
					return
				}
				expected := f.id
				if mismatch == "session" {
					expected = "other-session"
				} else {
					start = start.Add(-time.Second)
				}
				if err = f.s.store.BindClaudeScreenContext(old.DecisionID, "p1", expected, start); err != nil {
					t.Error(err)
				}
				f.s.screenPrompts.watches = map[int]*screenPromptWatch{1: {prompt: p, signature: p.Signature(), decisionID: old.DecisionID, lastMatched: true}}
			})
			after := review2After(t, old.CreatedUnixMS)
			f.transcript(t, f.question(old, "next", after, old.Title))
			if r := answerScreen(f.s, old, "1", nil); r.Err == nil {
				t.Error("answer accepted with mismatched context")
			}
			identity, err := f.s.store.ScreenQuestionIdentity(old.DecisionID)
			if err != nil || identity != "" {
				t.Errorf("identity changed: %q %v", identity, err)
			}
			data, _ := os.ReadFile(f.input)
			if len(data) > 0 {
				t.Errorf("input sent: %q", data)
			}
		})
	}
}
