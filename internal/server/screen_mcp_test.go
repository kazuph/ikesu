package server

import (
	"os"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"

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
