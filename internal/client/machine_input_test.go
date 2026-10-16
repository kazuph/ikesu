package client

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"golang.org/x/sys/unix"
)

func TestMachineInputFramesSplitSequencesAndClosesOwnedReader(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	flags, err := unix.FcntlInt(r.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, stop, err := readMachineInput(r, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err = w.Write([]byte("\x1b[<0;")); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("4;8M\x1b[200~payload\x1b[201~")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []input.Kind{input.KindMouseClick, input.KindPaste} {
		select {
		case event := <-events:
			if event.Kind != want {
				t.Fatalf("event=%+v want=%v", event, want)
			}
			if want == input.KindPaste && string(eventBytes(event)) != "\x1b[200~payload\x1b[201~" {
				t.Fatal("paste bytes changed")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("input not delivered")
		}
	}
	finished := make(chan struct{})
	go func() { stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("owned input reader remained blocked")
	}
	after, err := unix.FcntlInt(r.Fd(), unix.F_GETFL, 0)
	if err != nil || flags != after {
		t.Fatal("original descriptor flags not restored", flags, after, err)
	}
}

func TestMachineInputWaitsForMouseAndFramesEscapeOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, stop, err := readMachineInput(r, ctx)
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

func TestMachineWriterPreservesOrderWithoutBlockingOtherEndpoint(t *testing.T) {
	blockedRead, blockedWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer blockedRead.Close()
	defer blockedWrite.Close()
	peer := endpoint.Current()
	blocked := &endpointConnection{writer: &writer{conn: blockedWrite, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	blockedContext, stopBlocked := context.WithCancel(context.Background())
	defer stopBlocked()
	blockedDone := make(chan error, 1)
	go func() { blockedDone <- blocked.writeQueued(blockedContext) }()
	if err := blocked.enqueue(msg{Type: "input", Data: strings.Repeat("x", 1<<20)}); err != nil {
		t.Fatal(err)
	}
	blockedRead.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(blockedRead, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	// This endpoint is writing a payload larger than the actual OS pipe;
	// leave its reader undrained while the other endpoint delivers input.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	c := &endpointConnection{writer: &writer{conn: w, available: peer.Generation, methods: peer.Methods}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.writeQueued(ctx) }()
	for _, message := range []msg{{Type: "resize", Cols: 160, Rows: 45}, {Type: "workspace_select", Workspace: 7}, {Type: "input", Data: "bG9jYWw="}} {
		if err := c.enqueue(message); err != nil {
			t.Fatal(err)
		}
	}
	r.SetReadDeadline(time.Now().Add(5 * time.Second))
	decoder := json.NewDecoder(r)
	for _, want := range []string{"resize", "workspace_select", "input"} {
		var message msg
		if err := decoder.Decode(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type != want {
			t.Fatalf("write order=%s want=%s", message.Type, want)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not stop")
	}
	stopBlocked()
	blockedWrite.Close()
	select {
	case <-blockedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked endpoint writer survived close")
	}
}

func TestMachineSwitchTimeoutLeavesCurrentViewAndInputUntouched(t *testing.T) {
	start := time.Unix(1, 0)
	m := &machineClient{active: "local", endpoints: map[string]*managedEndpoint{"local": {}, "remote": {}}, pending: &pendingMachine{id: "remote", workspace: 7, deadline: start.Add(machineSwitchWait)}}
	m.advance(start.Add(machineSwitchWait - time.Nanosecond))
	if m.pending == nil || m.active != "local" {
		t.Fatal("switch cancelled before five seconds")
	}
	m.advance(start.Add(machineSwitchWait))
	if m.pending != nil || m.active != "local" || m.notice != "" {
		t.Fatal("timeout changed current view or emitted connection progress notification")
	}
}
