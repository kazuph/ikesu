package runtime

// Exited closes after the pane process has exited.
func (p *Pane) Exited() <-chan struct{} { return p.done }
