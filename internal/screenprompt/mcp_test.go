package screenprompt

import (
	"github.com/kazuph/ikesu/internal/api"
	"os"
	"strings"
	"testing"
)

// Contract examples built from SPEC; real-screen provenance is verified separately.
func mcpPicture(server, question string) []string {
	lines := []string{`MCP server "` + server + `" requests your input`, ""}
	lines = append(lines, strings.Split(question, "\n")...)
	return append(lines, "", "❯ Accept   Decline", "", "Esc to cancel · ↑/↓ to navigate")
}

func TestMCPConfirmationIdentityAndControls(t *testing.T) {
	p := Parse("claude", mcpPicture("first", "May the server continue?"))
	if p == nil || p.Params.Kind != "tool_permission" || p.MCPServer != "first" || p.Params.AllowText || p.Params.MultiSelect || p.Cursor != 0 {
		t.Fatalf("%+v", p)
	}
	if p.Params.Options[0].Role != "approve" || p.Params.Options[1].Role != "reject" || len(p.Keys) != 0 {
		t.Fatal(p.Params.Options, p.Keys)
	}
	other := Parse("claude", mcpPicture("second", p.Params.Title))
	if other == nil || p.Signature() == other.Signature() || p.ChoiceSignature() == other.ChoiceSignature() {
		t.Fatal("MCP server not part of identity")
	}
	d := api.Decision{Title: p.Params.Title, Kind: p.Params.Kind, Options: p.Params.Options, Origin: &api.DecisionOrigin{Agent: p.OriginAgent("claude")}}
	if FromDecision(d) != p.Signature() {
		t.Fatal("persisted MCP identity lost")
	}
	decline := mcpPicture("first", p.Params.Title)
	decline[4] = "  Accept  ❯ Decline"
	q := Parse("claude", decline)
	if q == nil || q.Cursor != 1 || q.Signature() != p.Signature() {
		t.Fatal(q)
	}
}

func TestMCPUnsupportedAndStaleHistoryAreNotConfirmations(t *testing.T) {
	for _, button := range []string{"Accept Decline", "❯ Accept  ❯ Decline", "❯ Accept\nDecline", "❯ Accept   Decline  Extra"} {
		screen := mcpPicture("test", "Continue?")
		screen[4] = button
		if p := Parse("claude", screen); p != nil && p.MCPServer != "" {
			t.Fatal(button, p)
		}
	}
	for _, question := range []string{"", "[ ] Multiple inputs", "1. First field\n2. Second field", "☐ Text field"} {
		if p := Parse("claude", mcpPicture("test", question)); p != nil && p.MCPServer != "" {
			t.Fatal(question, p)
		}
	}
	old := mcpPicture("old", "Old confirmation?")
	next := []string{"New question?", "❯ 1. First", "  2. Second"}
	p := Parse("claude", append(old, next...))
	if p == nil || p.MCPServer != "" || p.Params.Title != "New question?" || strings.Contains(p.Params.Title, "old") {
		t.Fatalf("old MCP mixed into next question: %+v", p)
	}
}

// Parent captured both screens from Claude Code v2.1.307, codex-cu's real
// TextEdit permission request, and authorized keeping them as test fixtures.
func TestMCPCapturedClaude21307Screens(t *testing.T) {
	var previous *Prompt
	for cursor, name := range []string{"accept", "decline"} {
		data, err := os.ReadFile("testdata/claude-mcp-" + name + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		p := Parse("claude", strings.Split(string(data), "\n"))
		if p == nil || p.Params.Kind != "tool_permission" || p.Params.Title != `Allow Computer Use to use "TextEdit"?` || p.MCPServer != "codex-cu" || p.OriginAgent("claude") != "claude / MCP: codex-cu" || p.Cursor != cursor || p.Params.AllowText || p.Params.MultiSelect {
			t.Fatalf("captured %s: %+v", name, p)
		}
		if len(p.Params.Options) != 2 || p.Params.Options[0].Label != "Accept" || p.Params.Options[0].Role != "approve" || p.Params.Options[1].Label != "Decline" || p.Params.Options[1].Role != "reject" {
			t.Fatal(p.Params.Options)
		}
		if previous != nil && previous.Signature() != p.Signature() {
			t.Fatal("cursor movement changed captured question identity")
		}
		previous = p
	}
}
