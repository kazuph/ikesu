//go:build !darwin

package runtime

// Other supported platforms retain their existing process observation path.
func processEventQueue() bool                   { return false }
func (p *Pane) watchProcessChanges(int, ...int) {}
func (p *Pane) beginProcessWatch()              { p.queueProcessChange() }

func (p *Pane) endProcessWatch() { p.forgetProcessSchedule() }
