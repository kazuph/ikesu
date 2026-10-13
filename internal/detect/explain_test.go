package detect

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplainMatchesDetector(t *testing.T) {
	for _, agent := range AgentIDs() {
		for _, in := range []Input{{}, {Screen: "allow command?\n"}, {OSCTitle: "Action Required"}, {Screen: "↑/↓ to scroll pgup/pgdn to home/end to jump q to quit esc to edit prev"}, {Screen: "• Working (3s • esc to interrupt)\n› \n"}} {
			d, e := Detect(agent, in), Explain(agent, in)
			if e.State != d.State.String() || e.SkipStateUpdate != d.SkipStateUpdate || e.VisibleIdle != d.VisibleIdle || e.VisibleWorking != d.VisibleWorking || e.VisibleBlocker != d.VisibleBlocker {
				t.Fatalf("%s: %+v != %+v", agent, d, e)
			}
			if d.RuleID != "" && (e.MatchedRule == nil || e.MatchedRule.ID != d.RuleID) {
				t.Fatalf("%s matched rule lost", agent)
			}
			if e.ManifestSource == nil || *e.ManifestSource != "bundled" || e.ManifestVersion == nil || *e.ManifestVersion == "" {
				t.Fatalf("%s manifest missing", agent)
			}
			if e.CachedRemoteVersion != nil || e.RemoteUpdateStatus != nil {
				t.Fatal("invented remote data")
			}
		}
	}
}

func TestExplainObservedContract(t *testing.T) {
	e := Explain("codex", Input{Screen: "allow command?\n"})
	if e.State != "blocked" || e.MatchedRule == nil || e.MatchedRule.ID != "live_strong_blocker" {
		t.Fatal(e)
	}
	data, _ := json.Marshal(e.MatchedRule)
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	if len(fields) != 4 {
		t.Fatalf("matched rule shape: %s", data)
	}
	e = Explain("missing", Input{})
	if e.State != "unknown" || e.ManifestSource != nil || *e.FallbackReason != "unknown_agent" || len(e.EvaluatedRules) != 0 {
		t.Fatal(e)
	}
	e = Explain("codex", Input{Screen: "↑/↓ to scroll pgup/pgdn to home/end to jump q to quit esc to edit prev"})
	if !e.SkipStateUpdate || *e.SkippedUpdateReason != "matched_rule:transcript_viewer" {
		t.Fatal(e)
	}
	if got := explainPreview(strings.Repeat("猫", 241)); got != strings.Repeat("猫", 240)+"..." {
		t.Fatal(got)
	}
}
