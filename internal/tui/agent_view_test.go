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

func TestDecisionAttentionViewerAndShellTransitions(t *testing.T) {
	a := specRuntimeApp(t)
	a.SetReported(1, "", "")
	v := a.NewViewer(nil, 80, 24, nil)
	var states []string
	a.SetAvailHook(func(pid int) { states = append(states, a.PaneStatusKey(pid)) })
	a.SetDecisionAttention(map[int]bool{1: true})
	v.with(a, func(ui *App) {
		if ui.PaneStatusKey(1) != "blocked" || ui.glyph(1).Rune != "●" {
			t.Error("endpoint viewer lost attention")
		}
	})
	if len(states) != 2 || states[0] != "unknown" || states[1] != "blocked" {
		t.Fatal(states)
	}
	sequence := a.AgentStateChangeSeq(1)
	a.SetReported(1, "claude", "blocked")
	a.SetDecisionAttention(nil)
	if a.AgentStateChangeSeq(1) != sequence {
		t.Fatal("underlying blocked duplicated transition")
	}
	a.SetDecisionAttention(map[int]bool{1: true})
	if a.AgentStateChangeSeq(1) != sequence {
		t.Fatal("already blocked duplicated transition")
	}
}
