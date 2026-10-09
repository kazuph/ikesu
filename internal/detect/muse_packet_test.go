package detect

import "testing"

func TestMusePacketAliasesStatesAndPickerNonCollision(t *testing.T) {
	for _, name := range []string{"muse", "muse-bin", "muse-code", "muse-cli"} {
		if got := Identify(name, name); got != "muse" {
			t.Fatalf("%s = %s", name, got)
		}
	}
	cases := []struct {
		screen string
		state  State
	}{
		{"⟩", Idle},
		{"Thinking esc to interrupt", Working},
		{"Allow once\nAllow for this session", Blocked},
		{"Choose an option\nEnter to select\nTab for an optional note", Blocked},
		{"Pick tools\nEnter to toggle\nEsc to interrupt", Blocked},
	}
	for _, tc := range cases {
		if got := Detect("muse", Input{Screen: tc.screen}); got.State != tc.state {
			t.Fatalf("%q = %+v", tc.screen, got)
		}
	}
	picker := Input{Screen: cases[3].screen}
	for _, other := range []string{"codex", "claude", "devin", "qwen", "letta", "grok", "agy"} {
		if got := Detect(other, picker); got.State == Blocked {
			t.Fatalf("muse picker blocked %s: %+v", other, got)
		}
	}
	if got := Identify("codex", "codex"); got != "codex" {
		t.Fatal(got)
	}
}
