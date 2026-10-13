package runtime

import "os"

// SessionEnvironment identifies session-file roots for explicit pane launch
// settings. It does not expose them through the public pane API.
func (p *Pane) SessionEnvironment() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Env != nil {
		return append([]string(nil), p.cmd.Env...)
	}
	return os.Environ()
}
