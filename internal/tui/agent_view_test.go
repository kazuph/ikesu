package tui

import "testing"

func TestAgentStateChangeSequenceTracksTransitions(t *testing.T) {
	a := specRuntimeApp(t)
	if a.AgentStateChangeSeq(1) != 0 {
		t.Fatal("unknown initial state fabricated a transition")
	}
	a.SetReported(1, "codex", "blocked")
	first := a.AgentStateChangeSeq(1)
	if first == 0 {
		t.Fatal("state transition not recorded")
	}
	a.SetReported(1, "codex", "blocked")
	if a.AgentStateChangeSeq(1) != first {
		t.Fatal("duplicate state created transition")
	}
	a.SetReported(2, "qwen", "working")
	second := a.AgentStateChangeSeq(2)
	if second <= first {
		t.Fatal(first, second)
	}
	a.SetReported(1, "codex", "idle")
	if a.AgentStateChangeSeq(1) <= second {
		t.Fatal("sequence is not monotonic")
	}
}
