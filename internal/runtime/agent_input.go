package runtime

import (
	"context"
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// Exited closes after the pane process has exited.
func (p *Pane) Exited() <-chan struct{} { return p.done }

// InputOperation reserves one turn for a text-and-key submission.
type InputOperation struct {
	pane     *Pane
	ready    chan struct{}
	active   bool
	finished bool
	reserved int
}

func (p *Pane) ReserveInputOperation(textBytes int, keys []uv.KeyEvent) (*InputOperation, error) {
	p.inputOperationMu.Lock()
	defer p.inputOperationMu.Unlock()
	reserved := textBytes
	if textBytes > 0 {
		reserved += len(ansi.BracketedPasteStart) + len(ansi.BracketedPasteEnd)
	}
	for _, key := range keys {
		reserved += vt.MaxKeySequenceLen(key)
	}
	p.writer.mu.Lock()
	if p.writer.stopped != nil {
		err := p.writer.stopped
		p.writer.mu.Unlock()
		return nil, err
	}
	if reserved > ptyInputLimit-p.writer.queued-p.writer.reserved {
		p.writer.mu.Unlock()
		return nil, ErrPTYInputFull
	}
	p.writer.reserved += reserved
	p.writer.mu.Unlock()
	op := &InputOperation{pane: p, ready: make(chan struct{}), reserved: reserved}
	if !p.inputOperationBusy {
		p.inputOperationBusy = true
		p.inputOperationIdle = make(chan struct{})
		op.active = true
		close(op.ready)
	} else {
		p.inputOperations = append(p.inputOperations, op)
	}
	return op, nil
}

// SendInputOperationKeys transfers reserved key bytes to the PTY queue.
func (p *Pane) SendInputOperationKeys(op *InputOperation, expected uint64, keys []uv.KeyEvent, record bool) error {
	var writeErr error
	write := func() {
		p.WithEmu(func(e *vt.Emulator) {
			var data []byte
			for _, key := range keys {
				data = append(data, e.KeySequence(key)...)
			}
			writeErr = p.writer.enqueueReserved(data, nil, nil, op)
		})
	}
	var admitted bool
	if record {
		_, admitted = p.RecordKeyInputAndDo(expected, write)
	} else {
		admitted = p.WithInputSequence(expected, write)
	}
	if !admitted {
		return fmt.Errorf("pane input interrupted submission")
	}
	return writeErr
}

// PasteInputOperation transfers an admitted reservation to the PTY queue.
func (p *Pane) PasteInputOperation(op *InputOperation, expected uint64, text string) error {
	var writeErr error
	_, admitted := p.RecordKeyInputAndDo(expected, func() {
		p.WithEmu(func(e *vt.Emulator) {
			if e.ModeSet(ansi.ModeBracketedPaste) {
				text = ansi.BracketedPasteStart + text + ansi.BracketedPasteEnd
			}
			writeErr = p.writer.enqueueReserved([]byte(text), nil, nil, op)
		})
	})
	if !admitted {
		return fmt.Errorf("pane input interrupted submission")
	}
	return writeErr
}

// InputOperationsIdle returns a channel closed when accepted operations finish.
// A nil channel means there are no accepted operations.
func (p *Pane) InputOperationsIdle() <-chan struct{} {
	p.inputOperationMu.Lock()
	defer p.inputOperationMu.Unlock()
	if !p.inputOperationBusy {
		return nil
	}
	return p.inputOperationIdle
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
	p.writer.mu.Lock()
	p.writer.reserved -= op.reserved
	op.reserved = 0
	p.writer.mu.Unlock()
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
		close(p.inputOperationIdle)
		return
	}
	next := p.inputOperations[0]
	p.inputOperations[0] = nil
	p.inputOperations = p.inputOperations[1:]
	next.active = true
	close(next.ready)
}
