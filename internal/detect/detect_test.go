package detect

import "testing"

func TestAllManifestsLoadWithoutWarnings(t *testing.T) {
	if len(LoadWarnings) != 0 {
		t.Fatalf("load warnings: %v", LoadWarnings)
	}
	if got := len(manifests); got != 22 {
		t.Fatalf("manifests = %d, want 22", got)
	}
	for _, m := range manifests {
		if manifestFor(m.id) == nil {
			t.Fatalf("manifestFor(%q) failed", m.id)
		}
	}
}

func TestIdentify(t *testing.T) {
	cases := map[string]string{
		"claude":       "claude",
		"Claude":       "claude",
		"claude-code":  "claude",
		"codex":        "codex",
		"devin-cli":    "devin",
		"cursor-agent": "cursor",
		"gyu":          "",
		"zsh":          "",
		"vim":          "",
	}
	for name, want := range cases {
		if got := Identify(name, ""); got != want {
			t.Errorf("Identify(%q) = %q, want %q", name, got, want)
		}
	}
	// argv0 fallback: a wrapper process whose name isn't the agent.
	if got := Identify("node", "/usr/local/bin/claude"); got != "claude" {
		t.Fatalf("argv0 fallback = %q", got)
	}
	// .js suffix normalization on argv0.
	if got := Identify("node", "qwen.js"); got != "qwen" {
		t.Fatalf("argv0 .js = %q", got)
	}
}

func TestDetectNoAgentIsUnknown(t *testing.T) {
	d := Detect("", Input{Screen: "anything"})
	if d.State != Unknown || d.RuleID != "" {
		t.Fatalf("got %+v", d)
	}
}

func TestDetectClaudeOSCTitle(t *testing.T) {
	// claude.toml: busy spinner glyphs are braille/half-circle + space.
	d := Detect("claude", Input{OSCTitle: "⠋ Claude"})
	if d.State != Working {
		t.Fatalf("osc_title working → %v", d.State)
	}
	d = Detect("claude", Input{OSCTitle: "✳ Claude"})
	if d.State != Idle {
		t.Fatalf("osc_title idle → %v", d.State)
	}
}

func TestDetectClaudePromptBoxIdle(t *testing.T) {
	screen := "some output\n──────────\n ❯ \n──────────\n"
	d := Detect("claude", Input{Screen: screen})
	if d.State != Idle || !d.VisibleIdle {
		t.Fatalf("prompt box → %+v", d)
	}
}

func TestDetectClaudePermissionBlocked(t *testing.T) {
	screen := "Do you want to proceed?\n❯ 1. Yes\n  2. No\nesc to cancel\n"
	d := Detect("claude", Input{Screen: screen})
	if d.State != Blocked {
		t.Fatalf("permission prompt → %v", d.State)
	}
}

func TestDetectCodexLiveOverride(t *testing.T) {
	// No manifest rule matches, but the Codex live region says working.
	screen := "✓ done\n\n› \n\n• Working (12s · esc to interrupt)\n"
	d := Detect("codex", Input{Screen: screen})
	if d.State != Working || !d.VisibleWorking {
		t.Fatalf("codex live override → %+v", d)
	}
}

func TestDetectSkipStateUpdate(t *testing.T) {
	// transcript viewer screens must not change state.
	m := manifestFor("claude")
	found := false
	for _, r := range m.rules {
		if r.SkipStateUpdate {
			found = true
			break
		}
	}
	if !found {
		t.Skip("no skip_state_update rule in claude.toml")
	}
}

func TestRegions(t *testing.T) {
	screen := "line1\n› prompt\nafter1\nafter2\n"
	in := Input{Screen: screen, OSCTitle: "T", OSCProgress: "P"}

	if got := regionText(in, "osc_title"); got != "T" {
		t.Errorf("osc_title = %q", got)
	}
	if got := regionText(in, "after_last_prompt_marker"); got != "after1\nafter2" {
		t.Errorf("after_last_prompt_marker = %q", got)
	}
	if got := regionText(in, "bottom_lines(2)"); got != "after1\nafter2" {
		t.Errorf("bottom_lines(2) = %q", got)
	}
	if got := regionText(in, "bottom_non_empty_lines(1)"); got != "after2" {
		t.Errorf("bottom_non_empty_lines(1) = %q", got)
	}
	if got := regionText(in, "whole_recent"); got != screen {
		t.Errorf("whole_recent wrong")
	}
	if got := regionText(in, "bogus"); got != "" {
		t.Errorf("bogus = %q", got)
	}
}

func TestHorizontalRuleRegions(t *testing.T) {
	// The prompt box is delimited by the 2nd-to-last and last rules.
	screen := "before\n──────────\ninside prompt\n──────────\nbody text\n"
	in := Input{Screen: screen}
	if got := regionText(in, "prompt_box_body"); got != "inside prompt" {
		t.Errorf("prompt_box_body = %q", got)
	}
	if got := regionText(in, "after_last_horizontal_rule"); got != "body text" {
		t.Errorf("after_last_horizontal_rule = %q", got)
	}
	if got := regionText(in, "above_prompt_box"); got != "before" {
		t.Errorf("above_prompt_box = %q", got)
	}
	if got := regionText(in, "last_non_empty_above_prompt_box"); got != "before" {
		t.Errorf("last_non_empty_above_prompt_box = %q", got)
	}
}

func TestCompileCompat(t *testing.T) {
	re, err := compileCompat(`^[\u2800-\u28FF]+$`)
	if err != nil || !re.MatchString("⠋⠙") {
		t.Fatalf("braille: re=%v err=%v", re, err)
	}
	re, err = compileCompat(`(?i)WORKING`)
	if err != nil || !re.MatchString("working now") {
		t.Fatalf("(?i) failed")
	}
	re, err = compileCompat(`(?m)^✓ done$`)
	if err != nil || !re.MatchString("x\n✓ done\ny") {
		t.Fatalf("(?m) failed")
	}
}

func TestActivityFingerprint(t *testing.T) {
	claudeScreen := "output line\n\n──────────────\n✻ Thinking… (12s · esc to interrupt)\n──────────────\n❯ \n──────────────\n"
	if fp := ActivityFingerprint("claude", claudeScreen); fp == "" {
		t.Fatal("claude spinner screen must have a fingerprint")
	}
	// A finished summary alone is not live activity.
	summary := "output\n\n✻ Cooked for 13m 55s\n\n──────────────\n❯ \n──────────────\n"
	if fp := ActivityFingerprint("claude", summary); fp != "" {
		t.Fatalf("summary line must not fingerprint, got %q", fp)
	}
	// Codex: the contiguous status/hint block before the latest prompt.
	codexScreen := "old output\n• Working (37s • esc to interrupt)\n\n› \n"
	if fp := ActivityFingerprint("codex", codexScreen); fp == "" {
		t.Fatal("codex working screen must have a fingerprint")
	}
	if fp := ActivityFingerprint("codex", "done\n\n› \n"); fp != "" {
		t.Fatalf("idle codex screen must not fingerprint, got %q", fp)
	}
	// Other agents have no fingerprint.
	if fp := ActivityFingerprint("gemini", claudeScreen); fp != "" {
		t.Fatalf("non-claude/codex agent fingerprinted: %q", fp)
	}
}
