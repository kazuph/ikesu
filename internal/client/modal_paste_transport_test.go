package client

import (
	"testing"
)

func TestModalPasteUnsupportedPeerRejectsWithoutQueueing(t *testing.T) {
	c := &endpointConnection{writer: &writer{available: 1, methods: []string{"input"}}, wake: make(chan struct{}, 1)}
	m := &machineClient{endpoints: map[string]*managedEndpoint{"local": {connection: c}}}
	err := m.sendModalPaste("local", "alpha\x1b[201~\rX")
	if err == nil || err.Error() != "paste unsupported" || len(c.queue) != 0 {
		t.Fatal("unsupported paste sent", err, c.queue)
	}
}
