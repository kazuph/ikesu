package runtime

import "github.com/kazuph/goterrace/internal/guard"

// SetGuard applies only to later spawns, just like SetShell.
func (s *Supervisor) SetGuard(c guard.Config, root string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guardConfig = &c
	s.guardRoot = root
}
