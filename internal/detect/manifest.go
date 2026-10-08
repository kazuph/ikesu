package detect

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

// manifestFile mirrors one src/detect/manifests/*.toml file.
type manifestFile struct {
	ID      string   `toml:"id"`
	Version string   `toml:"version"`
	Aliases []string `toml:"aliases"`
	Rules   []rule   `toml:"rules"`
}

type rule struct {
	ID              string   `toml:"id"`
	State           string   `toml:"state"`
	Priority        int      `toml:"priority"`
	Region          string   `toml:"region"`
	VisibleIdle     bool     `toml:"visible_idle"`
	VisibleBlocker  bool     `toml:"visible_blocker"`
	VisibleWorking  bool     `toml:"visible_working"`
	SkipStateUpdate bool     `toml:"skip_state_update"`
	All             []gate   `toml:"all"`
	Any             []gate   `toml:"any"`
	Not             []gate   `toml:"not"`
	Contains        []string `toml:"contains"`
	Regex           []string `toml:"regex"`
	LineRegex       []string `toml:"line_regex"`

	gate compiledGate
}

type gate struct {
	All       []gate   `toml:"all"`
	Any       []gate   `toml:"any"`
	Not       []gate   `toml:"not"`
	Contains  []string `toml:"contains"`
	Regex     []string `toml:"regex"`
	LineRegex []string `toml:"line_regex"`
}

type compiledGate struct {
	all       []compiledGate
	any       []compiledGate
	not       []compiledGate
	contains  []string // pre-lowered needles
	regex     []*regexp.Regexp
	lineRegex []*regexp.Regexp
}

type manifest struct {
	id      string
	aliases []string
	rules   []rule // compiled gates attached
}

// manifests is the parsed + compiled manifest set, keyed by id.
var manifests []*manifest

// LoadWarnings collects non-fatal load problems (unparseable rules are
// disabled, never silently treated as matching).
var LoadWarnings []string

func init() {
	entries, err := manifestFS.ReadDir("manifests")
	if err != nil {
		LoadWarnings = append(LoadWarnings, err.Error())
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".toml") {
			continue
		}
		data, err := manifestFS.ReadFile("manifests/" + e.Name())
		if err != nil {
			LoadWarnings = append(LoadWarnings, err.Error())
			continue
		}
		var mf manifestFile
		if err := toml.Unmarshal(data, &mf); err != nil {
			LoadWarnings = append(LoadWarnings, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		m := &manifest{id: mf.ID, aliases: mf.Aliases}
		for _, r := range mf.Rules {
			if !knownRegion(r.Region) {
				LoadWarnings = append(LoadWarnings, fmt.Sprintf("%s: rule %s: unknown region %q", e.Name(), r.ID, r.Region))
				continue
			}
			cg, err := compileGate(gate{All: r.All, Any: r.Any, Not: r.Not, Contains: r.Contains, Regex: r.Regex, LineRegex: r.LineRegex})
			if err != nil {
				LoadWarnings = append(LoadWarnings, fmt.Sprintf("%s: rule %s: %v", e.Name(), r.ID, err))
				continue
			}
			r.gate = cg
			m.rules = append(m.rules, r)
		}
		manifests = append(manifests, m)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].id < manifests[j].id })
}

// manifestFor finds the manifest for a canonical id.
func manifestFor(id string) *manifest {
	for _, m := range manifests {
		if m.id == id {
			return m
		}
	}
	return nil
}

func compileGate(g gate) (compiledGate, error) {
	cg := compiledGate{}
	for _, s := range g.Contains {
		cg.contains = append(cg.contains, strings.ToLower(s))
	}
	for _, p := range g.Regex {
		re, err := compileCompat(p)
		if err != nil {
			return cg, fmt.Errorf("regex %q: %w", p, err)
		}
		cg.regex = append(cg.regex, re)
	}
	for _, p := range g.LineRegex {
		re, err := compileCompat(p)
		if err != nil {
			return cg, fmt.Errorf("line_regex %q: %w", p, err)
		}
		cg.lineRegex = append(cg.lineRegex, re)
	}
	for _, sub := range g.All {
		c, err := compileGate(sub)
		if err != nil {
			return cg, err
		}
		cg.all = append(cg.all, c)
	}
	for _, sub := range g.Any {
		c, err := compileGate(sub)
		if err != nil {
			return cg, err
		}
		cg.any = append(cg.any, c)
	}
	for _, sub := range g.Not {
		c, err := compileGate(sub)
		if err != nil {
			return cg, err
		}
		cg.not = append(cg.not, c)
	}
	return cg, nil
}

func gateMatches(g *compiledGate, text, lower string) bool {
	for _, needle := range g.contains {
		if !strings.Contains(lower, needle) {
			return false
		}
	}
	for _, re := range g.regex {
		if !re.MatchString(text) {
			return false
		}
	}
	for _, re := range g.lineRegex {
		if !anyLineMatches(text, re) {
			return false
		}
	}
	for _, sub := range g.all {
		if !gateMatches(&sub, text, lower) {
			return false
		}
	}
	if len(g.any) > 0 {
		ok := false
		for i := range g.any {
			if gateMatches(&g.any[i], text, lower) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, sub := range g.not {
		if gateMatches(&sub, text, lower) {
			return false
		}
	}
	return true
}

var (
	reUnicodeBraced = regexp.MustCompile(`\\u\{([0-9A-Fa-f]+)\}`)
	reUnicodeShort  = regexp.MustCompile(`\\u([0-9A-Fa-f]{4})`)
	reAlphabetic    = regexp.MustCompile(`\\p\{Alphabetic\}`)
)

// compileCompat adapts manifest patterns (written for Rust's `regex`) to
// Go's RE2 syntax: \u{XXXX} / \uXXXX → \x{XXXX}, \p{Alphabetic} → \pL.
// (?i) and (?m) work natively.
func compileCompat(pattern string) (*regexp.Regexp, error) {
	p := reUnicodeBraced.ReplaceAllString(pattern, `\x{$1}`)
	p = reUnicodeShort.ReplaceAllString(p, `\x{$1}`)
	p = reAlphabetic.ReplaceAllString(p, `\pL`)
	return regexp.Compile(p)
}

func anyLineMatches(text string, re *regexp.Regexp) bool {
	for _, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// detect evaluates every rule; highest priority match wins (first wins
// ties). No match → Idle.
func (m *manifest) detect(in Input) Detection {
	var best *rule
	for i := range m.rules {
		r := &m.rules[i]
		text := regionText(in, r.Region)
		if !gateMatches(&r.gate, text, strings.ToLower(text)) {
			continue
		}
		if best == nil || r.Priority > best.Priority {
			best = r
		}
	}
	if best == nil {
		return Detection{State: Idle}
	}
	d := Detection{State: parseState(best.State), SkipStateUpdate: best.SkipStateUpdate, RuleID: best.ID}
	d.VisibleIdle = best.VisibleIdle && d.State == Idle
	d.VisibleBlocker = best.VisibleBlocker && d.State == Blocked
	d.VisibleWorking = best.VisibleWorking && d.State == Working
	return d
}

func parseState(s string) State {
	switch s {
	case "idle":
		return Idle
	case "working":
		return Working
	case "blocked":
		return Blocked
	}
	return Unknown
}
