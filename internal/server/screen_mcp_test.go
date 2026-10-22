package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

func mcpScreen(server, question string, decline bool) string {
	name := "accept"
	if decline {
		name = "decline"
	}
	data, err := os.ReadFile("../screenprompt/testdata/claude-mcp-" + name + ".txt")
	if err != nil {
		panic(err)
	}
	picture := strings.ReplaceAll(string(data), "codex-cu", server)
	return strings.ReplaceAll(picture, `Allow Computer Use to use "TextEdit"?`, question)
}

func TestMCPRealPTYArrowThenVerifiedEnter(t *testing.T) {
	for _, change := range []string{"correct", "server", "question", "disappeared", "unreadable", "external-input", "cursor"} {
		t.Run(change, func(t *testing.T) {
			teststate.Lock(t)
			s, p, input := screenTerminal(t, "claude-single", "claude")
			picture := mcpScreen("first", "May we continue?", false)
			paintScreen(p, picture)
			scanScreen(s)
			d := screenRecord(t, s)
			if d.Origin == nil || d.Origin.Agent != "claude / MCP server “first”" || d.Kind != "tool_permission" || d.AllowText {
				t.Fatal(d)
			}
			var prompt *screenprompt.Prompt
			s.app.Do(func() { prompt = s.screenPrompts.watches[1].prompt; s.beginScreenFlowAnswer(1, prompt) })
			id := "2"
			done := make(chan error, 1)
			go func() {
				done <- s.deliverScreenAnswer(1, prompt.Signature(), prompt, api.DecisionAnswer{OptionID: &id})
			}()
			waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "\x1b[B" })
			after := mcpScreen("first", "May we continue?", true)
			switch change {
			case "server":
				after = mcpScreen("second", "May we continue?", true)
			case "question":
				after = mcpScreen("first", "Different question?", true)
			case "disappeared":
				after = "ordinary idle screen"
			case "unreadable":
				after = "MCP server \"first\" requests your input\nMay we continue?\nAccept Decline"
			case "external-input":
				p.RecordExternalInput()
			case "cursor":
				after = picture
			}
			paintScreen(p, after)
			err := <-done
			if change == "correct" {
				if err != nil {
					t.Fatal(err)
				}
				waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "\x1b[B\r" })
			} else {
				if err == nil {
					t.Fatal("unsafe confirmation succeeded")
				}
				if data, _ := os.ReadFile(input); string(data) != "\x1b[B" {
					t.Fatalf("extra bytes after %s: %q", change, data)
				}
			}
		})
	}
}

func TestMCPServerChangeDoesNotReuseOrFloodUpdateOldDecision(t *testing.T) {
	teststate.Lock(t)
	s, p, _ := screenTerminal(t, "claude-single", "claude")
	var previous api.Decision
	for _, server := range []string{"first", "second", "third", "fourth", "fifth"} {
		paintScreen(p, mcpScreen(server, "Same truncated question…", false))
		scanScreen(s)
		scanScreen(s)
		d := screenRecord(t, s)
		if d.DecisionID == previous.DecisionID || d.Origin.Agent != "claude / MCP server “"+server+"”" {
			t.Fatal("server change reused old decision", d, previous)
		}
		if previous.DecisionID != "" {
			old, err := s.store.Decision(previous.DecisionID)
			if err != nil || old.Status != "cancelled" {
				t.Fatal(old, err)
			}
		}
		previous = d
	}
	paintScreen(p, mcpScreen("old", "Old confirmation?", false)+"\nNew question?\n❯ 1. First\n  2. Second\n")
	scanScreen(s)
	scanScreen(s)
	d := screenRecord(t, s)
	if d.Title != "New question?" || strings.Contains(d.Origin.Agent, "MCP") {
		t.Fatal(d)
	}
}

func TestPreviewAnswerReadsFiveTimesBeforeGivingUp(t *testing.T) {
	for _, mode := range []string{"immediate", "late", "never", "question-changed"} {
		t.Run(mode, func(t *testing.T) {
			teststate.Lock(t)
			s, p, input := screenTerminal(t, "claude-preview", "claude")
			var prompt *screenprompt.Prompt
			s.app.Do(func() {
				prompt = s.readScreenPrompt(1)
				s.screenPrompts.watches = map[int]*screenPromptWatch{}
				s.screenPrompts.watches[1] = &screenPromptWatch{prompt: prompt, signature: prompt.Signature(), resolved: true, decisionID: "delivering"}
				s.beginScreenFlowAnswer(1, prompt)
			})
			target := "2"
			start := time.Now()
			done := make(chan error, 1)
			go func() {
				done <- s.deliverScreenAnswer(1, prompt.Signature(), prompt, api.DecisionAnswer{OptionID: &target})
			}()
			waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "2" })
			if mode == "late" {
				time.Sleep(2*agentPromptSubmitDelay + agentPromptSubmitDelay/2)
			}
			if mode != "never" {
				picture := screenDialog(t, "claude-preview")
				picture = strings.ReplaceAll(picture, "❯ 1. Short", "  1. Short")
				picture = strings.ReplaceAll(picture, "  2. Detailed", "❯ 2. Detailed")
				if mode == "question-changed" {
					picture = strings.ReplaceAll(picture, "Which draft should we keep?", "Which different draft should we keep?")
				}
				paintScreen(p, picture)
			}
			err := <-done
			if mode == "immediate" || mode == "late" {
				if err != nil {
					t.Fatal(err)
				}
				waitPromptCondition(t, func() bool { data, _ := os.ReadFile(input); return string(data) == "2\r" })
			} else {
				if err == nil {
					t.Fatal("unsafe preview answer succeeded")
				}
				if data, _ := os.ReadFile(input); string(data) != "2" {
					t.Fatalf("unexpected confirmation after %s: %q", mode, data)
				}
				if mode == "never" && time.Since(start) < previewAnswerReadLimit*agentPromptSubmitDelay {
					t.Fatal("gave up before all five read intervals")
				}
			}
			t.Logf("%s: elapsed=%s error=%v", mode, time.Since(start), err)
		})
	}
}
