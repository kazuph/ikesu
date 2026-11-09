package runtime

import "context"

// Exited closes after the pane process has exited.
func (p *Pane) Exited() <-chan struct{} { return p.done }

// InputOperation reserves one turn for a text-and-key submission.
type InputOperation struct {
	pane     *Pane
	ready    chan struct{}
	active   bool
	finished bool
}

func (p *Pane) ReserveInputOperation() *InputOperation {
	p.inputOperationMu.Lock()
	defer p.inputOperationMu.Unlock()
	op := &InputOperation{pane: p, ready: make(chan struct{})}
	if !p.inputOperationBusy {
		p.inputOperationBusy = true
		op.active = true
		close(op.ready)
	} else {
		p.inputOperations = append(p.inputOperations, op)
	}
	return op
}

// Wait is called outside the app loop. Canceled reservations are removed.
func (op *InputOperation) Wait(ctx context.Context) bool {
	select {
	case <-op.ready:
		select {
		case <-op.pane.done:
		default:
			if ctx.Err() == nil {
				return true
			}
		}
	case <-ctx.Done():
	case <-op.pane.done:
	}
	op.Release()
	return false
}

func (op *InputOperation) Release() {
	p := op.pane
	p.inputOperationMu.Lock()
	defer p.inputOperationMu.Unlock()
	if op.finished {
		return
	}
	op.finished = true
	if !op.active {
		for i, queued := range p.inputOperations {
			if queued == op {
				p.inputOperations = append(p.inputOperations[:i], p.inputOperations[i+1:]...)
				break
			}
		}
		return
	}
	if len(p.inputOperations) == 0 {
		p.inputOperationBusy = false
		return
	}
	next := p.inputOperations[0]
	p.inputOperations[0] = nil
	p.inputOperations = p.inputOperations[1:]
	next.active = true
	close(next.ready)
}
