package client

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/machine"
)

func TestMachineInputWaitsForMouseAndFramesEscapeOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, stop, err := readMachineInput(r, ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := w.Write([]byte("\x1b[<0;70;")); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		t.Fatalf("incomplete mouse emitted %+v", event)
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := w.Write([]byte("20M")); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Kind != input.KindMouseClick || string(event.Raw) != "\x1b[<0;70;20M" {
			t.Fatalf("split mouse=%+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("split mouse not delivered")
	}
	start := time.Now()
	if _, err := w.Write([]byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if string(event.Raw) != "\x1b[27u" || time.Since(start) < time.Duration(input.EndpointEscapeFlushMs)*time.Millisecond {
			t.Fatalf("endpoint Escape=%+v elapsed=%v", event, time.Since(start))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Escape not delivered")
	}
}

func TestMachineInputStopRetainsQueuedEvents(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &machineInputState{framer: input.Framer{EscapeWaitMs: input.EndpointEscapeFlushMs}}
	events, stop, err := readMachineInputWithState(r, ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("z", queuedInputBatchLimit+10)
	if _, err := w.WriteString(want); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	select {
	case event := <-events:
		got.Write(event.Raw)
	case <-time.After(5 * time.Second):
		t.Fatal("input reader did not start")
	}
	stop()
	for event := range events {
		got.Write(event.Raw)
	}
	for _, event := range state.unsent {
		got.Write(event.Raw)
	}
	if got.String() != want {
		t.Fatalf("input changed on stop: got %d bytes, want %d", got.Len(), len(want))
	}
}

// Real sockets block an in-flight message, then the verified executable is actually replaced before recovery.
func TestUpgradeFailureReplaysUnsentSocketInputOnce(t *testing.T) {
	server, stream := shutdownSocketPair(t)
	peer := endpoint.Current()
	original := &endpointConnection{writer: &writer{conn: stream, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- original.writeQueued(ctx) }()
	if err := original.enqueue(msg{Type: "input", Data: "Zmlyc3Q="}); err != nil {
		t.Fatal(err)
	}
	var first msg
	if err := json.NewDecoder(server).Decode(&first); err != nil {
		t.Fatal(err)
	}
	if first.Data != "Zmlyc3Q=" {
		t.Fatal("first input was not delivered")
	}
	large := strings.Repeat("e", 1<<20)
	if err := original.enqueue(msg{Type: "input", Data: large}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(server, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := original.enqueue(msg{Type: "input", Data: "dGFpbA=="}); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".ikesu-upgrade-queue-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	info, sum, err := executableIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	checked := checkedExecutable{path: path, info: info, sum: sum}
	cancel()
	stream.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not stop")
	}
	m := &machineClient{session: "local", settings: &localSettings{}, endpoints: map[string]*managedEndpoint{"remote": {profile: machine.Profile{ID: "remote", Target: "host", Session: "other"}, connection: original}}}
	m.saveUpgradeQueues()
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := checked.recheck(); err == nil {
		t.Fatal("replaced executable was not rejected")
	}
	nextServer, nextStream := shutdownSocketPair(t)
	next := &endpointConnection{writer: &writer{conn: nextStream, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	m.endpoints["remote"].connection = next
	if err := m.replayUpgradeQueue("remote"); err != nil {
		t.Fatal(err)
	}
	if err := m.replayUpgradeQueue("remote"); err != nil {
		t.Fatal(err)
	}
	nextCtx, nextCancel := context.WithCancel(context.Background())
	nextDone := make(chan error, 1)
	go func() { nextDone <- next.writeQueued(nextCtx) }()
	decoder := json.NewDecoder(nextServer)
	for _, want := range []string{large, "dGFpbA=="} {
		var got msg
		if err := decoder.Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Data != want {
			t.Fatal("unsent input was lost, reordered, or duplicated")
		}
	}
	if err := nextServer.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var duplicate msg
	if err := decoder.Decode(&duplicate); err == nil {
		t.Fatalf("duplicate input: %+v", duplicate)
	}
	nextCancel()
	nextStream.Close()
	<-nextDone
}
