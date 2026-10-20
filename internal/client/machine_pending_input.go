package client

import (
	"bytes"
	"time"

	"github.com/kazuph/ikesu/internal/input"
)

// Reference limits for keys waiting on a saved-machine switch (stage8 T5).
const (
	pendingInputWait  = 3 * time.Second
	pendingInputLimit = 1024
)

func (p *pendingMachine) inputDeadline() time.Time {
	return p.deadline.Add(pendingInputWait - machineSwitchWait)
}

func (m *machineClient) holdPendingInput(event input.Event, now time.Time) bool {
	p := m.pending
	if p == nil || (event.Kind != input.KindKey && event.Kind != input.KindPaste && event.Kind != input.KindUnknown) {
		return false
	}
	if !now.Before(p.inputDeadline()) {
		p.keys = nil
		return true
	}
	if len(p.keys) < pendingInputLimit {
		event.Raw = bytes.Clone(event.Raw)
		p.keys = append(p.keys, event)
	}
	// An expired/full destination queue must never send these keys to the
	// still-visible old machine. The reference does not guarantee excess input.
	return true
}
