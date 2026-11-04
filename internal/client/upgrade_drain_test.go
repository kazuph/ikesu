package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
)

func TestPrepareUpgradeDefersHeldInputUntilClipboardCompletes(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	h := newRightMachineHarness(t)
	m := h.m
	m.active = "remote"
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	m.readClipboard = func(ctx context.Context) (clipboardContent, error) {
		select {
		case <-ready:
			return clipboardContent{Text: ""}, nil
		case <-ctx.Done():
			return clipboardContent{}, ctx.Err()
		}
	}
	if err := m.pasteClipboard(); err != nil {
		t.Fatal(err)
	}
	var f input.Framer
	result := filepath.Join(t.TempDir(), "paste-count")
	m.held = normalizeMachineInput(f.Push([]byte(fmt.Sprintf("printf x >> %q; printf 'PASTE_HELD_ONCE\\n'\n", result))))
	incoming := make(chan input.Event)
	close(incoming)
	if err := m.prepareUpgrade(m.ctx, &machineInputState{}, incoming); err == nil {
		t.Error("upgrade succeeded with clipboard input held")
	}
	close(ready)
	h.until("clipboard input", func() bool { data, _ := os.ReadFile(result); return len(data) > 0 })
	if data, err := os.ReadFile(result); err != nil || string(data) != "x" {
		t.Fatalf("paste input count: %q, %v", data, err)
	}
	if strings.Contains(paneText(m, "local"), "PASTE_HELD_ONCE") {
		t.Fatal("input reached local")
	}
}

func paneText(m *machineClient, id string) string {
	var b strings.Builder
	for _, row := range m.endpoints[id].snapshot.Panes[1].Grid {
		for _, cell := range row {
			fmt.Fprint(&b, cell.Content)
		}
	}
	return b.String()
}

func TestPrepareUpgradeDefersInputUntilLocalSnapshot(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	h := newRightMachineHarness(t)
	m := h.m
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	e := m.endpoints["local"]
	e.restarting = true
	var f input.Framer
	result := filepath.Join(t.TempDir(), "restart-count")
	m.held = normalizeMachineInput(f.Push([]byte(fmt.Sprintf("printf x >> %q; printf 'RESTART_HELD_ONCE\\n'\n", result))))
	incoming := make(chan input.Event)
	close(incoming)
	if err := m.prepareUpgrade(m.ctx, &machineInputState{}, incoming); err == nil {
		t.Error("upgrade succeeded before local snapshot")
	}
	m.event(endpointEvent{id: "local", message: msg{Type: "snapshot", snapshot: e.snapshot}})
	h.until("restart input", func() bool { data, _ := os.ReadFile(result); return len(data) > 0 })
	if data, err := os.ReadFile(result); err != nil || string(data) != "x" {
		t.Fatalf("restart input count: %q, %v", data, err)
	}
	if strings.Contains(paneText(m, "remote"), "RESTART_HELD_ONCE") {
		t.Fatal("input reached remote")
	}
}

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

// Two real servers and their pane PTYs are needed to verify the original destination.
func TestPrepareUpgradeDeliversAlreadyReadInputToOriginalEndpoint(t *testing.T) {
	if teststate.RunIsolatedParallel(t) {
		return
	}
	h := newRightMachineHarness(t)
	m := h.m
	m.active = "remote"
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	var framer input.Framer
	m.held = normalizeMachineInput(framer.Push([]byte("printf 'DRAIN_%s\\n' 'REMOTE_ONLY'\n")))
	incoming := make(chan input.Event)
	close(incoming)
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()
	if err := m.prepareUpgrade(ctx, &machineInputState{}, incoming); err != nil {
		t.Fatal(err)
	}
	content := func(id string) string {
		var out strings.Builder
		for _, row := range m.endpoints[id].snapshot.Panes[1].Grid {
			for _, cell := range row {
				fmt.Fprint(&out, cell.Content)
			}
		}
		return out.String()
	}
	h.until("drained remote input", func() bool { return strings.Contains(content("remote"), "DRAIN_REMOTE_ONLY") })
	if strings.Contains(content("local"), "DRAIN_REMOTE_ONLY") {
		t.Fatal("input reached local endpoint")
	}
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
