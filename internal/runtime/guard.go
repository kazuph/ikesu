package runtime

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/guard"
	"sort"
	"strings"
)

// SetGuard updates assets as well as later spawns. An existing process with
// no injected environment is reported instead of pretending it is covered.
func (s *Supervisor) SetGuard(c guard.Config, root string) {
	s.mu.Lock()
	s.guardConfig, s.guardRoot = &c, root
	panes := make([]*Pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	for _, p := range panes {
		if p.guard != nil {
			_ = p.guard.Reconfigure(c)
		}
		p.mu.Lock()
		effective, dead := p.effectiveAgent(), p.dead
		p.mu.Unlock()
		if p.guard != nil && !dead {
			_ = p.guard.Sync(effective)
			p.queueProcessChange()
		}
	}
}
func (s *Supervisor) GuardWarnings() string {
	s.mu.Lock()
	panes := make([]*Pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	sort.Slice(panes, func(i, j int) bool { return panes[i].ID < panes[j].ID })
	var warnings []string
	for _, p := range panes {
		if p.guard != nil {
			if issue := p.guard.Issue(); issue != "" {
				warnings = append(warnings, issue)
			}
		}
	}
	return strings.Join(warnings, "; ")
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
		p.queueProcessChange()
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

func (s *Supervisor) RebindGuardSocket(socket string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var changed []GuardCheckpoint
	panes := make([]*Pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].ID < panes[j].ID })
	for _, p := range panes {
		if p.guard == nil {
			continue
		}
		checkpoint, err := p.guard.Checkpoint()
		if err != nil {
			return rollbackGuardCheckpoints(changed, err)
		}
		if err = p.guard.RebindSocket(socket); err != nil {
			return rollbackGuardCheckpoints(changed, err)
		}
		changed = append(changed, GuardCheckpoint{p.guard, checkpoint})
	}
	return nil
}

type GuardCheckpoint struct {
	assets *guard.Assets
	state  *guard.RuntimeCheckpoint
}

func rollbackGuardCheckpoints(checkpoints []GuardCheckpoint, failure error) error {
	for n := len(checkpoints) - 1; n >= 0; n-- {
		c := checkpoints[n]
		if err := c.assets.RestoreCheckpoint(c.state); err != nil {
			if failure == nil {
				failure = fmt.Errorf("guard restore failed: %w", err)
			} else {
				failure = fmt.Errorf("%w; guard restore failed: %v", failure, err)
			}
		}
	}
	return failure
}
func (s *Supervisor) CaptureGuardState() ([]GuardCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []GuardCheckpoint
	for _, p := range s.panes {
		if p.guard != nil {
			state, err := p.guard.Checkpoint()
			if err != nil {
				return nil, err
			}
			result = append(result, GuardCheckpoint{p.guard, state})
		}
	}
	return result, nil
}
func (s *Supervisor) RestoreGuardState(checkpoints []GuardCheckpoint) error {
	return rollbackGuardCheckpoints(checkpoints, nil)
}
