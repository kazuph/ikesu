package client

import "github.com/kazuph/ikesu/internal/input"

// T4's observed bound is the first input plus at most 512 queued inputs.
const queuedInputBatchLimit = 512

func (m *machineClient) drainInput(first input.Event, queued <-chan input.Event) (int, error) {
	m.inputBatch = true
	count, err := m.applyInputBatch(first, queued)
	m.inputBatch = false
	if err != nil {
		return count, err
	}
	if m.paintQueued {
		return count, m.paint()
	}
	return count, nil
}

func (m *machineClient) applyInputBatch(event input.Event, queued <-chan input.Event) (int, error) {
	for count := 1; ; count++ {
		if err := m.input(event); err != nil {
			return count, err
		}
		// A pending destination must be allowed to receive its first frame.
		if m.pending != nil || count > queuedInputBatchLimit {
			return count, nil
		}
		select {
		case next, ok := <-queued:
			if !ok {
				return count, nil
			}
			event = next
		default:
			return count, nil
		}
	}
}
