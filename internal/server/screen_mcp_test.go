package server

import (
	"os"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/screenprompt"
	"github.com/kazuph/ikesu/internal/teststate"
)

func mcpScreen(server, question string, decline bool) string {
	buttons := "❯ Accept   Decline"
	if decline {
		buttons = "  Accept ❯ Decline"
	}
	return "MCP server \"" + server + "\" requests your input\n\n" + question + "\n\n" + buttons + "\n\nEsc to cancel · ↑/↓ to navigate\n"
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
			if d.Origin == nil || d.Origin.Agent != "claude / MCP: first" || d.Kind != "tool_permission" || d.AllowText {
				t.Fatal(d)
			}
			var prompt *screenprompt.Prompt
			s.app.Do(func() { prompt = s.screenPrompts.watches[1].prompt; s.beginScreenFlowAnswer(1, prompt) })
			id := "decline"
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
		if d.DecisionID == previous.DecisionID || d.Origin.Agent != "claude / MCP: "+server {
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
