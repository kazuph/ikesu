package runtime

import (
	"github.com/kazuph/goterrace/internal/guard"
	"strings"
)

// SetGuard applies only to later spawns, just like SetShell.
func (s *Supervisor) SetGuard(c guard.Config, root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guardConfig = &c
	s.guardRoot = root
}

// SetAgent overrides the detected agent id (pane.report_agent). An empty
// id clears the override back to detection-driven state.
func (p *Pane) SetAgent(id string) {
	p.mu.Lock()
	p.agentOverride = id
	if id == "" {
		p.guardStarted = ""
	}
	effective := p.effectiveAgent()
	dead := p.dead
	p.mu.Unlock()
	if p.guard != nil && !dead {
		_ = p.guard.Sync(effective)
	}
}

// effectiveAgent reports the override when set, else the detected id.
// Call with p.mu held.
func (p *Pane) effectiveAgent() string {
	if p.agentOverride != "" {
		return p.agentOverride
	}
	return p.agentID
}

// cleanEnv drops NO_COLOR from the inherited environment: a parent set
// NO_COLOR to mute its own output, and pane programs still need color
// (SPEC G6: NO_COLOR をペインへ引き継がない).
func cleanEnv(env []string) []string {
	env = guard.RestoreInherited(env)
	out := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "NO_COLOR=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
