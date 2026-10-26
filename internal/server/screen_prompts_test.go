package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

func screenDialog(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../screenprompt/testdata/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Exercise the real PTY, terminal emulator and SQLite. cat records actual input bytes;
// no AI process or service is substituted. Real-agent acceptance is recorded separately.
func screenTerminal(t *testing.T, name, agent string) (*Server, *runtime.Pane, string) {
	t.Helper()
	s := newTestServer(t, "screen")
	teststate.Setenv(t, "SHELL", "/bin/sh")
	dir := t.TempDir()
	input := filepath.Join(dir, "keys")
	output := filepath.Join(dir, "dialog")
	picture := screenDialog(t, name)
	if err := os.WriteFile(output, []byte("\x1b[2J\x1b[H"+strings.ReplaceAll(picture, "\n", "\r\n")), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.sup.SpawnWith(1, dir, 160, 60, nil, []string{"/bin/sh", "-c", `stty raw -echo; cat "$1"; exec cat > "$2"`, "screen", output, input})
	if err != nil {
		t.Fatal(err)
	}
	p.SetAgent(agent)
	s.app.SetReported(1, agent, "blocked")
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
	lines := strings.Split(strings.TrimSpace(picture), "\n")
	footer := strings.TrimSpace(lines[len(lines)-1])
	// The first option can arrive before its preview panel and footer.
	// Answer tests must begin with the complete fixture.
	waitPromptCondition(t, func() bool {
		text := p.ScreenText()
		return strings.Contains(text, "1.") && strings.Contains(text, footer)
	})
	return s, p, input
}

func scanScreen(s *Server) { s.app.Do(s.scanScreenPrompts) }
func screenRecord(t *testing.T, s *Server) api.Decision {
	t.Helper()
	var d api.Decision
	var err error
	s.app.Do(func() {
		w := s.screenPrompts.watches[1]
		if w == nil {
			t.Error("missing watch")
			return
		}
		d, err = s.store.Decision(w.decisionID)
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func paintScreen(p *runtime.Pane, picture string) {
	p.WithEmu(func(e *vt.Emulator) {
		_, _ = e.Write([]byte("\x1b[2J\x1b[H" + strings.ReplaceAll(picture, "\n", "\r\n")))
	})
}
func answerScreen(s *Server, d api.Decision, id string, text *string) api.Response {
	p := api.DecisionAnswerParams{DecisionID: d.DecisionID, Text: text}
	if id != "" {
		p.OptionID = &id
	}
	return s.dispatch(&api.Request{ID: "answer", Method: api.MethodDecisionAnswer, Params: api.MarshalParams(p)})
}

func TestScreenPromptTwoReadsIdentityAndResolution(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	if d.Origin == nil || d.Origin.Reason != "screen_prompt" || d.Origin.Machine != "local" || d.Origin.PaneID != "p1" {
		t.Fatal(d)
	}
	scanScreen(s)
	if screenRecord(t, s).DecisionID != d.DecisionID {
		t.Fatal("duplicate")
	}
	paintScreen(p, "one incomplete read")
	scanScreen(s)
	if screenRecord(t, s).Status != "pending" {
		t.Fatal("cancelled on first read")
	}
	paintScreen(p, screenDialog(t, "claude-single"))
	scanScreen(s)
	if screenRecord(t, s).DecisionID != d.DecisionID {
		t.Fatal("did not recover")
	}
	paintScreen(p, strings.ReplaceAll(screenDialog(t, "claude-single"), "A compact plain text report", "A changed plain text report"))
	scanScreen(s)
	scanScreen(s)
	new := screenRecord(t, s)
	if new.DecisionID == d.DecisionID {
		t.Fatal("description change reused old decision")
	}
	old, err := s.store.Decision(d.DecisionID)
	if err != nil || old.Status != "cancelled" {
		t.Fatal(old, err)
	}
	s.app.Do(func() {
		resp := s.decisionRequest(&api.Request{ID: "cancel", Method: api.MethodDecisionCancel, Params: api.MarshalParams(api.DecisionTargetParams{DecisionID: new.DecisionID})})
		if resp.Err != nil {
			t.Error(resp.Err)
		}
	})
	scanScreen(s)
	scanScreen(s)
	all, err := s.store.Decisions("")
	if err != nil || len(all) != 2 {
		t.Fatalf("recreated resolved question: %d %v", len(all), err)
	}
	paintScreen(p, "")
	scanScreen(s)
	scanScreen(s)
	s.app.Do(func() {
		s.app.SetReported(1, "claude", "idle")
		if len(s.screenPrompts.watches) != 0 || s.screenPromptsActive() {
			t.Error("idle retained screen watch")
		}
	})
}

func TestScreenPromptStaleAnswersSendNoKeys(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-single", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	paintScreen(p, strings.ReplaceAll(screenDialog(t, "claude-single"), "Which output format", "Which different format"))
	resp := answerScreen(s, d, "1", nil)
	if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
		t.Fatal(resp)
	}
	got, err := s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatal(got, err)
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatalf("stale input: %q", keys)
	}
}

func TestScreenPromptDisappearedAndMissingWatchAreStale(t *testing.T) {
	teststate.Lock(t)
	for _, missingWatch := range []bool{false, true} {
		s, p, input := screenTerminal(t, "claude-single", "claude")
		scanScreen(s)
		d := screenRecord(t, s)
		if missingWatch {
			s.app.Do(func() { s.screenPrompts.watches = nil })
		} else {
			paintScreen(p, "ordinary idle screen")
		}
		resp := answerScreen(s, d, "1", nil)
		if resp.Err == nil || resp.Err.Code != api.ErrDecisionStale {
			t.Fatalf("missingWatch=%t: %+v", missingWatch, resp)
		}
		if keys, _ := os.ReadFile(input); len(keys) != 0 {
			t.Fatal("stale key sent", keys)
		}
		got, err := s.store.Decision(d.DecisionID)
		if err != nil || got.Status != "cancelled" {
			t.Fatal(got, err)
		}
	}
}

func TestScreenPromptShortcutsTextAndInvalidAnswers(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "codex-permission", "codex")
	scanScreen(s)
	d := screenRecord(t, s)
	resp := answerScreen(s, d, "unknown", nil)
	if resp.Err == nil {
		t.Fatal("invalid option accepted")
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal(keys)
	}
	resp = answerScreen(s, d, "3", nil)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\x1b" })
	paintScreen(p, screenDialog(t, "claude-single"))
	s.app.Do(func() { p.SetAgent("claude"); s.app.SetReported(1, "claude", "blocked") })
	scanScreen(s)
	scanScreen(s)
	d = screenRecord(t, s)
	text := "My custom answer"
	resp = answerScreen(s, d, "", &text)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\x1b3My custom answer\r" })
}

func TestScreenPromptPendingReuseAndDisable(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-multi-wide", "claude")
	scanScreen(s)
	d := screenRecord(t, s)
	s.app.Do(func() { s.screenPrompts.watches = nil })
	scanScreen(s)
	if screenRecord(t, s).DecisionID != d.DecisionID {
		t.Fatal("pending not reused")
	}
	s.app.Do(func() {
		s.scheduleScreenPrompts()
		if s.screenPrompts.timer == nil {
			t.Error("active not scheduled")
		}
		c := config.Config{}
		disabled := false
		c.Decisions.ScreenPrompts = &disabled
		s.screenPromptConfig(c)
	})
	got, err := s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "cancelled" {
		t.Fatal(got, err)
	}
	s.app.Do(func() {
		if s.screenPrompts.timer != nil || len(s.screenPrompts.watches) != 0 || s.screenPromptsActive() {
			t.Error("disabled watch remained")
		}
	})
}

func TestScreenPromptUnsupportedMultiSelectKeepsPending(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-multi-wide", "claude")
	paintScreen(p, strings.ReplaceAll(screenDialog(t, "claude-multi-wide"), "     Next\n", ""))
	scanScreen(s)
	d := screenRecord(t, s)
	resp := answerScreen(s, d, "1", nil)
	if resp.Err == nil || resp.Err.Code != "screen_prompt_unsupported" {
		t.Fatal(resp)
	}
	got, err := s.store.Decision(d.DecisionID)
	if err != nil || got.Status != "pending" {
		t.Fatal(got, err)
	}
	if keys, _ := os.ReadFile(input); len(keys) != 0 {
		t.Fatal(keys)
	}
}

func TestScreenPromptExternallyExpiredDoesNotReappear(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := screenTerminal(t, "claude-single", "claude")
	var old api.Decision
	s.app.Do(func() {
		store, err := s.db()
		if err != nil {
			t.Error(err)
			return
		}
		p := s.readScreenPrompt(1).Params
		p.Origin = &api.DecisionOrigin{PaneID: "p1", Reason: "screen_prompt"}
		zero := uint64(0)
		p.TimeoutMS = &zero
		old, err = store.CreateDecision(p, time.Now())
		if err != nil {
			t.Error(err)
		}
	})
	scanScreen(s)
	if screenRecord(t, s).DecisionID != old.DecisionID {
		t.Fatal("pending not reused")
	}
	s.app.Do(func() {
		if err := s.expireDecisions(s.store); err != nil {
			t.Error(err)
		}
	})
	scanScreen(s)
	scanScreen(s)
	all, err := s.store.Decisions("")
	if err != nil || len(all) != 1 || all[0].Status != "expired" {
		t.Fatal(all, err)
	}
}

func TestScreenPreviewUnavailableAndReuseDoNotRescan(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-preview", "claude")
	scanScreen(s)
	s.app.Do(func() {
		w := s.screenPrompts.watches[1]
		w.preview.lastStep = time.Time{}
		// The real terminal did not move after the arrow: abort instead of guessing a preview.
		s.scanScreenPrompts()
	})
	d := screenRecord(t, s)
	if d.Options[0].Preview == nil || !d.Options[1].PreviewUnavailable {
		t.Fatal(d.Options)
	}
	var before []byte
	waitPromptCondition(t, func() bool { before, _ = os.ReadFile(input); return len(before) > 0 })
	s.app.Do(func() { s.screenPrompts.watches = nil })
	scanScreen(s)
	if screenRecord(t, s).DecisionID != d.DecisionID {
		t.Fatal("preview pending not reused")
	}
	if keys, _ := os.ReadFile(input); string(keys) != string(before) {
		t.Fatalf("re-scanned pending: %q -> %q", before, keys)
	}
	if prompt := screenprompt.Parse("claude", p.DetectionLines()); prompt == nil || prompt.Cursor != 0 {
		t.Fatal("cursor changed")
	}
}

func TestScreenPreviewOneTraversalAndRestoration(t *testing.T) {
	teststate.Lock(t)
	s, p, input := screenTerminal(t, "claude-preview", "claude")
	scanScreen(s)
	var w *screenPromptWatch
	s.app.Do(func() {
		w = s.screenPrompts.watches[1]
		if w == nil || w.preview == nil || w.decisionID != "" {
			t.Error("preview not deferred")
		}
	})
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\x1b[B" })
	picture := screenDialog(t, "claude-preview")
	picture = strings.ReplaceAll(picture, "❯ 1. Short", "  1. Short")
	picture = strings.ReplaceAll(picture, "  2. Detailed", "❯ 2. Detailed")
	picture = strings.ReplaceAll(picture, " A short report.", " Detailed draft.")
	paintScreen(p, picture)
	s.app.Do(func() { w.preview.lastStep = time.Time{}; s.scanScreenPrompts() })
	waitPromptCondition(t, func() bool { keys, _ := os.ReadFile(input); return string(keys) == "\x1b[B\x1b[A" })
	paintScreen(p, screenDialog(t, "claude-preview"))
	s.app.Do(func() { w.preview.lastStep = time.Time{}; s.scanScreenPrompts() })
	d := screenRecord(t, s)
	if d.Options[0].Preview == nil || d.Options[1].Preview == nil || d.Options[1].PreviewUnavailable {
		t.Fatal(d.Options)
	}
	scanScreen(s)
	scanScreen(s)
	if keys, _ := os.ReadFile(input); string(keys) != "\x1b[B\x1b[A" {
		t.Fatalf("repeated scan: %q", keys)
	}
	if prompt := screenprompt.Parse("claude", p.DetectionLines()); prompt == nil || prompt.Cursor != 0 {
		t.Fatal("cursor not restored")
	}
}

func TestScreenPromptCapabilitiesAndStatusJSON(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	ready := dialClient(t, dir)
	ready.conn.Close()
	defer stopServer(t, dir, done)
	var pong api.PongResult
	apiOK(t, dir, api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Capabilities["screen_prompt_decisions"] != true {
		t.Fatal(pong)
	}
	var wire map[string]any
	b, _ := json.Marshal(pong)
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
}

func TestScreenPromptNarrowRealQuestionUsesDetectedBlockedState(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-multi-narrow", "claude")
	if got := detect.Detect("claude", detect.Input{Screen: p.ScreenText()}); got.State != detect.Blocked || got.RuleID != "live_blocked_form" {
		t.Fatal(got)
	}
	s.app.Do(func() { s.app.SetReported(1, "claude", "") })
	waitPromptCondition(t, func() bool {
		var blocked bool
		s.app.Do(func() { blocked = s.app.PaneStatusKey(1) == "blocked" })
		return blocked
	})
	scanScreen(s)
	d := screenRecord(t, s)
	if d.Title != "Which sections should the report include?" || !d.MultiSelect || d.QuestionLabel == nil || *d.QuestionLabel != "質問 1/2 Sections" {
		t.Fatal(d)
	}
}
