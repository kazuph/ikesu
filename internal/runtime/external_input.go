package runtime

import (
	"fmt"
	"os"
	"time"
)

// ScreenPromptInputQuiet is specified by SPEC 工程6, not a UI debounce.
const ScreenPromptInputQuiet = 3 * time.Second

func (p *Pane) RecordExternalInput() {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	p.lastExternalInput.Store(time.Now().UnixNano())
	p.externalInputSeq.Add(1)
	p.manualInputSeq.Add(1)
}

// RecordKeyInput tracks externally supplied keys separately from screen automation.
func (p *Pane) RecordKeyInput() {
	p.recordKeyInput(true, nil, nil)
}

// RecordKeyInputAndDo admits input together with its activity sequence.
// fn must only enqueue input and must not wait for the PTY.
func (p *Pane) RecordKeyInputAndDo(expected uint64, fn func()) (uint64, bool) {
	return p.recordKeyInput(false, &expected, fn)
}

func (p *Pane) recordKeyInput(manual bool, expected *uint64, fn func()) (uint64, bool) {
	p.inputMu.Lock()
	if expected != nil && p.manualInputSeq.Load() != *expected {
		p.inputMu.Unlock()
		return 0, false
	}
	if manual {
		p.manualInputSeq.Add(1)
	}
	now := time.Now().UnixNano()
	p.lastExternalInput.Store(now)
	p.lastMailExternalInput.Store(now)
	p.externalInputSeq.Add(1)
	p.idleActivityAt = time.Unix(0, now)
	p.idleActivitySeq++
	seq := p.externalInputSeq.Load()
	if fn != nil {
		fn()
	}
	p.inputMu.Unlock()
	p.idleActivityChanged()
	return seq, true
}

func (p *Pane) WriteExternalInput(expected uint64, data []byte) (uint64, error) {
	p.inputMu.Lock()
	if p.manualInputSeq.Load() != expected {
		p.inputMu.Unlock()
		return 0, fmt.Errorf("pane input interrupted submission")
	}
	now := time.Now().UnixNano()
	p.lastExternalInput.Store(now)
	p.lastMailExternalInput.Store(now)
	seq := p.externalInputSeq.Add(1)
	done, err := p.queuePromptInputLocked(expected, data)
	p.inputMu.Unlock()
	if err == nil {
		err = <-done
	}
	return seq, err
}

func (p *Pane) idleActivityChanged() {
	if p.owner != nil && p.owner.IdleActivityChanged != nil {
		p.owner.IdleActivityChanged()
	}
}

func (p *Pane) RecordWorkingActivity() {
	p.inputMu.Lock()
	p.idleActivityAt = time.Now()
	p.idleActivitySeq++
	p.inputMu.Unlock()
	p.idleActivityChanged()
}

func (p *Pane) IdleActivity() (time.Time, uint64) {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	return p.idleActivityAt, p.idleActivitySeq
}

func (p *Pane) RestoreIdleActivity(at time.Time, seq uint64) {
	if at.IsZero() {
		at = time.Now()
	}
	p.inputMu.Lock()
	p.idleActivityAt, p.idleActivitySeq = at, seq
	p.inputMu.Unlock()
}
func (p *Pane) ExternalInputSequence() uint64 { return p.externalInputSeq.Load() }
func (p *Pane) ManualInputSequence() uint64   { return p.manualInputSeq.Load() }

// WithInputSequence admits a nonblocking input enqueue only if nobody typed
// since the caller captured seq.
func (p *Pane) WithInputSequence(seq uint64, fn func()) bool {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	if p.manualInputSeq.Load() != seq {
		return false
	}
	fn()
	return true
}

func (p *Pane) WritePTYIfInputUnchanged(seq uint64, data []byte) (int, error) {
	p.inputMu.Lock()
	if p.manualInputSeq.Load() != seq {
		p.inputMu.Unlock()
		return 0, fmt.Errorf("pane input interrupted submission")
	}
	done, err := p.queuePromptInputLocked(seq, data)
	p.inputMu.Unlock()
	if err != nil {
		return 0, err
	}
	if err := <-done; err != nil {
		return 0, err
	}
	return len(data), nil
}

// queuePromptInputLocked admits a complete write without waiting for the PTY.
func (p *Pane) queuePromptInputLocked(seq uint64, data []byte) (<-chan error, error) {
	if p.currentPTY() == nil {
		return nil, os.ErrClosed
	}
	done := make(chan error, 1)
	if err := p.writer.enqueue(data, done, func() bool { return p.manualInputSeq.Load() == seq }); err != nil {
		return nil, err
	}
	p.markProcessChanged()
	return done, nil
}
func (p *Pane) UserActive() bool {
	last := p.lastExternalInput.Load()
	return last != 0 && time.Since(time.Unix(0, last)) < ScreenPromptInputQuiet
}

// WriteScreenPrompt serializes the last activity check with external-input admission.
// WriteScreenPrompt accepts automated screen-prompt keys under the input
// lock without waiting for the application. They are written only if, when
// the PTY can take them, no external input arrived since and the deadline
// (when set) has not passed; otherwise they are dropped, never sent late.
func (p *Pane) WriteScreenPrompt(data []byte, sequence *uint64, deadline time.Time) (int, error) {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	if p.UserActive() || sequence != nil && p.ExternalInputSequence() != *sequence {
		return 0, fmt.Errorf("pane_user_active: external input interrupted screen prompt")
	}
	// Linux accepts writes to a PTY master after its child exits, so an exited
	// pane must count as a failed send instead of a delivered key.
	p.mu.Lock()
	dead := p.dead
	p.mu.Unlock()
	if dead {
		return 0, fmt.Errorf("pane exited: screen prompt key not delivered")
	}
	if p.currentPTY() == nil {
		return 0, os.ErrClosed
	}
	accepted := p.ExternalInputSequence()
	valid := func() bool {
		return p.ExternalInputSequence() == accepted && (deadline.IsZero() || time.Now().Before(deadline))
	}
	if err := p.writer.enqueue(data, nil, valid); err != nil {
		return 0, err
	}
	p.markProcessChanged()
	return len(data), nil
}

// WithIdleActivitySequence excludes new key input while fn validates and closes.
func (p *Pane) WithIdleActivitySequence(seq uint64, fn func() bool) bool {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	return p.idleActivitySeq == seq && fn()
}
