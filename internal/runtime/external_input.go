package runtime

import (
	"fmt"
	"time"
)

// ScreenPromptInputQuiet is specified by SPEC 工程6, not a UI debounce.
const ScreenPromptInputQuiet = 3 * time.Second

func (p *Pane) RecordExternalInput() {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	p.lastExternalInput.Store(time.Now().UnixNano())
	p.externalInputSeq.Add(1)
}

// RecordKeyInput tracks externally supplied keys separately from screen automation.
func (p *Pane) RecordKeyInput() {
	p.inputMu.Lock()
	now := time.Now().UnixNano()
	p.lastExternalInput.Store(now)
	p.lastMailExternalInput.Store(now)
	p.externalInputSeq.Add(1)
	p.idleActivityAt = time.Unix(0, now)
	p.idleActivitySeq++
	p.inputMu.Unlock()
	p.idleActivityChanged()
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
func (p *Pane) UserActive() bool {
	last := p.lastExternalInput.Load()
	return last != 0 && time.Since(time.Unix(0, last)) < ScreenPromptInputQuiet
}

// WriteScreenPrompt serializes the last activity check with external-input admission.
func (p *Pane) WriteScreenPrompt(data []byte, sequence *uint64) (int, error) {
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
	return p.WriteToPTY(data)
}
