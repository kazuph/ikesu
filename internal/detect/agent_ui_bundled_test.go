package detect

import "testing"

func TestG9BundledClaudeAndForkOwnedDevinContracts(t *testing.T) {
	if m := manifestFor("devin"); m == nil || !m.forkOwned {
		t.Fatal("Devin bundled ownership lost")
	}
	if m := manifestFor("claude"); m == nil || m.forkOwned {
		t.Fatal("Claude ownership changed")
	}
	for _, tc := range []struct {
		agent, screen string
		state         State
		rule          string
	}{
		{"claude", "Background shell is running\n──────────\n❯ \n──────────\n", Idle, "live_prompt_box"},
		{"claude", "MCP server \"test\" requests your input\n❯ Accept\n  Decline\nEsc to cancel\n", Blocked, "mcp_elicitation_prompt"},
		{"devin", "Running tools (esc twice to interrupt)\n❭\nContext: 12%\n", Working, "activity_footer_working"},
		{"devin", "Running tools (esc twice to\n interrupt)\n❭\nContext: 12%\n", Working, "activity_footer_working"},
		{"devin", "Queued messages\n────────\n❭\nContext: 12%\n", Idle, "live_prompt_footer"},
	} {
		d := Detect(tc.agent, Input{Screen: tc.screen})
		if d.State != tc.state || d.RuleID != tc.rule {
			t.Fatalf("%+v: %+v", tc, d)
		}
	}
}
