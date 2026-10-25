//go:build !darwin

package runtime

// Other supported platforms retain their existing process observation path.
func processEventQueue() bool           { return false }
func (p *Pane) watchProcessChanges(int) {}
func (p *Pane) beginProcessWatch()      {}

func (p *Pane) endProcessWatch() {}
