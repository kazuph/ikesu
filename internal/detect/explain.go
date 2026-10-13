package detect

import (
	"sort"
	"strings"
)

// Explanation is the public detection diagnostic; unavailable remote metadata
// remains null because this engine only loads bundled manifests.
type Explanation struct {
	Agent                        string          `json:"agent"`
	State                        string          `json:"state"`
	ManifestSource               *string         `json:"manifest_source"`
	ManifestVersion              *string         `json:"manifest_version"`
	CachedRemoteVersion          *string         `json:"cached_remote_version"`
	RemoteUpdateStatus           *string         `json:"remote_update_status"`
	RemoteUpdateError            *string         `json:"remote_update_error"`
	Warning                      *string         `json:"warning"`
	LocalOverrideShadowingRemote bool            `json:"local_override_shadowing_remote"`
	MatchedRule                  *MatchedRule    `json:"matched_rule"`
	EvaluatedRules               []EvaluatedRule `json:"evaluated_rules"`
	SkipStateUpdate              bool            `json:"skip_state_update"`
	SkippedUpdateReason          *string         `json:"skipped_update_reason"`
	FallbackReason               *string         `json:"fallback_reason"`
	ScreenDetectionSkipped       bool            `json:"screen_detection_skipped"`
	ScreenDetectionSkipReason    *string         `json:"screen_detection_skip_reason,omitempty"`
	VisibleIdle                  bool            `json:"visible_idle"`
	VisibleWorking               bool            `json:"visible_working"`
	VisibleBlocker               bool            `json:"visible_blocker"`
}

type MatchedRule struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Priority int    `json:"priority"`
	Region   string `json:"region"`
}

type EvaluatedRule struct {
	ID       string       `json:"id"`
	State    string       `json:"state"`
	Priority int          `json:"priority"`
	Region   string       `json:"region"`
	Matched  bool         `json:"matched"`
	Evidence RuleEvidence `json:"evidence"`
}

type RuleEvidence struct {
	AllCount      int      `json:"all_count"`
	AnyCount      int      `json:"any_count"`
	NotCount      int      `json:"not_count"`
	Contains      []string `json:"contains"`
	Regex         []string `json:"regex"`
	LineRegex     []string `json:"line_regex"`
	RegionBytes   int      `json:"region_bytes"`
	RegionPreview string   `json:"region_preview"`
}

func explainString(s string) *string { return &s }

// Explain uses the same compiled gates and region selection as Detect. It is
// computed on demand, never on the normal polling path.
func Explain(agent string, in Input) Explanation {
	d := Detect(agent, in)
	out := Explanation{Agent: agent, State: d.State.String(), EvaluatedRules: []EvaluatedRule{}, SkipStateUpdate: d.SkipStateUpdate, VisibleIdle: d.VisibleIdle, VisibleWorking: d.VisibleWorking, VisibleBlocker: d.VisibleBlocker}
	m := manifestFor(agent)
	if m == nil {
		out.FallbackReason = explainString("unknown_agent")
		return out
	}
	out.ManifestSource = explainString("bundled")
	out.ManifestVersion = explainString(m.version)
	for _, r := range m.rules {
		text := regionText(in, r.Region)
		e := EvaluatedRule{ID: r.ID, State: r.State, Priority: r.Priority, Region: r.Region, Matched: gateMatches(&r.gate, text, strings.ToLower(text)), Evidence: RuleEvidence{AllCount: len(r.All), AnyCount: len(r.Any), NotCount: len(r.Not), Contains: append([]string{}, r.Contains...), Regex: append([]string{}, r.Regex...), LineRegex: append([]string{}, r.LineRegex...), RegionBytes: len(text), RegionPreview: explainPreview(text)}}
		out.EvaluatedRules = append(out.EvaluatedRules, e)
	}
	sort.SliceStable(out.EvaluatedRules, func(i, j int) bool { return out.EvaluatedRules[i].Priority > out.EvaluatedRules[j].Priority })
	for i := range out.EvaluatedRules {
		if out.EvaluatedRules[i].ID == d.RuleID {
			r := out.EvaluatedRules[i]
			out.MatchedRule = &MatchedRule{ID: r.ID, State: r.State, Priority: r.Priority, Region: r.Region}
			break
		}
	}
	if d.SkipStateUpdate {
		out.SkippedUpdateReason = explainString("matched_rule:" + d.RuleID)
	}
	if agent == "codex" && d.State == Working && m.detect(in).State == Idle {
		out.FallbackReason = explainString("codex_live_activity_fingerprint")
	}
	if d.RuleID == "" && d.State == Idle {
		out.FallbackReason = explainString("default_known_agent_idle_fallback")
	}
	return out
}

// The distributed herdr diagnostic keeps the first 240 Unicode characters.
// Observed with 239/240/241 multibyte characters and a 1000-byte ASCII screen.
func explainPreview(text string) string {
	const previewCharacters = 240
	r := []rune(text)
	if len(r) > previewCharacters {
		return string(r[:previewCharacters]) + "..."
	}
	return text
}
