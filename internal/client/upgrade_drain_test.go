package client

import (
	"context"
	"encoding/json"
	"errors"

	"io"

	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
)

func TestUpgradeWaitEmptyPreservesBlockedSocketQueue(t *testing.T) {
	server, stream := shutdownSocketPair(t)
	peer := endpoint.Current()
	c := &endpointConnection{writer: &writer{conn: stream, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.writeQueued(ctx) }()
	if err := c.enqueue(msg{Type: "input", Data: strings.Repeat("e", 1<<20)}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(server, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	m := &machineClient{ctx: ctx, settings: &localSettings{}, active: "local", endpoints: map[string]*managedEndpoint{"local": {connection: c}}}
	incoming := make(chan input.Event)
	close(incoming)
	if err := m.prepareUpgrade(deadline, &machineInputState{}, incoming); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write: %v", err)
	}
	c.queueMu.Lock()
	pending := len(c.queue)
	c.queueMu.Unlock()
	if pending != 1 {
		t.Fatalf("pending input lost: %d", pending)
	}
	cancel()
	stream.Close()
	<-done
}

func TestUpgradeWaitEmptyCompletesAfterDelivery(t *testing.T) {
	server, stream := shutdownSocketPair(t)
	peer := endpoint.Current()
	c := &endpointConnection{writer: &writer{conn: stream, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.writeQueued(ctx) }()
	if err := c.enqueue(msg{Type: "input", Data: "YQ=="}); err != nil {
		t.Fatal(err)
	}
	var message msg
	if err := json.NewDecoder(server).Decode(&message); err != nil {
		t.Fatal(err)
	}
	if message.Data != "YQ==" {
		t.Fatalf("unexpected input: %+v", message)
	}
	deadline, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := c.waitEmpty(deadline); err != nil {
		t.Fatal(err)
	}
	cancel()
	stream.Close()
	<-done
}
