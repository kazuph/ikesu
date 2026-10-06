// Package detect identifies coding agents from process names and judges
// their state (idle / working / blocked) by applying the bundled TOML
// manifests to pane screen text and OSC state.
package detect

import (
	"embed"
	"strings"
)

//go:embed manifests/*.toml
var manifestFS embed.FS

// State is a pane's inferred agent state.
type State int

const (
	Idle State = iota
	Working
	Blocked
	Unknown
)

// String returns the lowercase state name used in manifests.
func (s State) String() string {
	switch s {
	case Idle:
		return "idle"
	case Working:
		return "working"
	case Blocked:
		return "blocked"
	}
	return "unknown"
}

// Input is everything rules can inspect for one pane.
type Input struct {
	Screen      string
	OSCTitle    string
	OSCProgress string
}

// Detection is the result of evaluating one agent's manifest.
type Detection struct {
	State           State
	VisibleIdle     bool
	VisibleWorking  bool
	VisibleBlocker  bool
	SkipStateUpdate bool
	RuleID          string // matched rule, "" when none
}

// Detect evaluates agent's manifest against in. agent "" or unknown →
// Unknown without touching any region.
func Detect(agent string, in Input) Detection {
	m := manifestFor(agent)
	if m == nil {
		return Detection{State: Unknown}
	}
	d := m.detect(in)
	// herdr parity: a Codex screen that says "Working (" / "esc to interrupt"
	// in its live region overrides an Idle verdict.
	if agent == "codex" && d.State == Idle && codexActivityFingerprint(in.Screen) != "" {
		d.State = Working
		d.VisibleIdle = false
		d.VisibleWorking = true
	}
	return d
}

// Identify maps a foreground process name (with argv0 basename fallback) to
// a canonical agent id; "" when it is not a known agent.
func Identify(procName, argv0 string) string {
	if id := lookupAgent(normalizedLookupName(procName)); id != "" {
		return id
	}
	base := argv0
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return lookupAgent(normalizedLookupName(base))
}

// AgentIDs returns all canonical manifest ids, sorted — for `gtx detect`.
func AgentIDs() []string {
	ids := make([]string, len(manifests))
	for i, m := range manifests {
		ids[i] = m.id
	}
	return ids
}

// normalizedLookupName lowercases and strips common suffixes (.exe etc.).
func normalizedLookupName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, suf := range []string{".exe", ".cmd", ".bat", ".ps1", ".js"} {
		if strings.HasSuffix(n, suf) {
			n = n[:len(n)-len(suf)]
			break
		}
	}
	return n
}

// lookupAgent maps a normalized process name to a canonical manifest id,
// covering the manifest aliases field.
func lookupAgent(name string) string {
	for _, m := range manifests {
		if m.id == name {
			return m.id
		}
		for _, al := range m.aliases {
			if al == name {
				return m.id
			}
		}
	}
	return ""
}
