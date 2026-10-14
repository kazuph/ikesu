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
