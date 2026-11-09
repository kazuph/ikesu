package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

func reserveTestInput(t *testing.T, p *runtime.Pane, size int, keys ...uv.KeyEvent) *runtime.InputOperation {
	t.Helper()
	op, err := p.ReserveInputOperation(size, keys)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestAgentPromptDisconnectReturnsWhilePTYIsFull(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	gate, ready, received := filepath.Join(dir, "gate"), filepath.Join(dir, "ready"), filepath.Join(dir, "received")
	if err := unix.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "shell")
	body := fmt.Sprintf("#!/bin/sh\nstty raw -echo; touch %q; read line < %q; exec cat > %q\n", ready, gate, received)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := newTestServer(t, "prompt-full-disconnect")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	waitPromptCondition(t, func() bool { _, err := os.Stat(ready); return err == nil })
	if err := p.QueuePTY([]byte(strings.Repeat("f", 128<<10))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	op := reserveTestInput(t, p, len("first"), uv.KeyPressEvent{Code: uv.KeyEnter})
	seq := p.ManualInputSequence()
	externalSeq := p.ExternalInputSequence()
	done := make(chan error, 1)
	go func() { done <- s.submitAgentPrompt(ctx, p, op, seq, "first", false) }()
	waitPromptCondition(t, func() bool { return p.ExternalInputSequence() > externalSeq })
	cancel()
	returned := false
	select {
	case <-done:
		returned = true
	case <-time.After(time.Second):
	}
	f, err := os.OpenFile(gate, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("go\n")
	_ = f.Close()
	if !returned {
		<-done
		t.Fatal("canceled prompt retained its PTY write wait")
	}
	waitPromptCondition(t, func() bool { data, _ := os.ReadFile(received); return strings.HasSuffix(string(data), "first\r") })
}

func TestPaneSendInputRejectsExcessReservedText(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "prompt-capacity")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	hold := reserveTestInput(t, p, 0)
	defer hold.Release()
	if !hold.Wait(context.Background()) {
		t.Fatal("initial input reservation failed")
	}
	body := strings.Repeat("x", 3<<20)
	for i := 0; i < 2; i++ {
		response := s.paneSendInput("test", api.PaneSendInputParams{PaneID: "p1", Text: body})
		if response.Err != nil {
			t.Fatalf("input %d rejected: %+v", i, response.Err)
		}
	}
	response := s.paneSendInput("test", api.PaneSendInputParams{PaneID: "p1", Text: body})
	if response.Err == nil {
		t.Fatal("accepted more than the PTY input limit in queued reservations")
	}
}

func TestAgentPromptDisconnectAfterBodyDoesNotCombineNextInput(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "lines")
	ready := filepath.Join(t.TempDir(), "ready")
	script := filepath.Join(t.TempDir(), "shell")
	body := fmt.Sprintf("#!/bin/sh\nstty echo; touch %q; while IFS= read -r line; do printf '%%s\\n' \"$line\" >> %q; done\n", ready, path)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := newTestServer(t, "prompt-disconnect-after-body")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	waitPromptCondition(t, func() bool { _, err := os.Stat(ready); return err == nil })
	seq := p.ManualInputSequence()
	first, second := reserveTestInput(t, p, len("first"), uv.KeyPressEvent{Code: uv.KeyEnter}), reserveTestInput(t, p, len("second"), uv.KeyPressEvent{Code: uv.KeyEnter})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doneA, doneB := make(chan error, 1), make(chan error, 1)
	go func() { doneA <- s.submitAgentPrompt(ctx, p, first, seq, "first", false) }()
	waitPromptCondition(t, func() bool { return strings.Contains(p.ScreenText(), "first") })
	cancel()
	go func() { doneB <- s.submitAgentPrompt(context.Background(), p, second, seq, "second", false) }()
	<-doneA
	if err := <-doneB; err != nil {
		t.Fatal(err)
	}
	var data []byte
	waitPromptCondition(t, func() bool { data, _ = os.ReadFile(path); return strings.HasSuffix(string(data), "second\n") })
	if string(data) != "first\nsecond\n" {
		t.Fatalf("combined input lines: %q", data)
	}
}

func TestConcurrentPaneSendInputRequestsRemainSeparate(t *testing.T) {
	teststate.Lock(t)
	root, err := os.MkdirTemp("/tmp", "ikesu-input-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	path := filepath.Join(root, "lines")
	ready := filepath.Join(root, "ready")
	script := filepath.Join(root, "shell")
	body := fmt.Sprintf("#!/bin/sh\nstty -echo; touch %q; while IFS= read -r line; do printf '%%s\\n' \"$line\" >> %q; done\n", ready, path)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	teststate.Setenv(t, "SHELL", script)
	dir, done := startServerAt(t, "input")
	defer stopServer(t, dir, done)
	waitPromptCondition(t, func() bool { _, err := os.Stat(ready); return err == nil })
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: "first", Keys: []string{"Enter"}}, nil)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: "second", Keys: []string{"Enter"}}, nil)
	var data []byte
	waitPromptCondition(t, func() bool { data, _ = os.ReadFile(path); return strings.Count(string(data), "\n") >= 2 })
	if string(data) != "first\nsecond\n" {
		t.Fatalf("mixed API input lines: %q", data)
	}
}

func TestConcurrentDelayedInputsRemainSeparate(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "lines")
	ready := filepath.Join(t.TempDir(), "ready")
	script := filepath.Join(t.TempDir(), "shell")
	body := fmt.Sprintf("#!/bin/sh\nstty -echo; touch %q; while IFS= read -r line; do printf '%%s\\n' \"$line\" >> %q; done\n", ready, path)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := newTestServer(t, "prompt-order")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	pid := s.app.State().Workspaces[0].Tabs[0].Layout.Focused()
	p := s.sup.Get(pid)
	waitPromptCondition(t, func() bool { _, err := os.Stat(ready); return err == nil })
	keys, bad := parseKeys([]string{"Enter"})
	if bad != "" {
		t.Fatal(bad)
	}
	seq := p.ExternalInputSequence()
	s.sendInputToPane(p, "first", keys)
	waitPromptCondition(t, func() bool { return p.ExternalInputSequence() >= seq+1 })
	s.sendInputToPane(p, "second", keys)
	var data []byte
	waitPromptCondition(t, func() bool { data, _ = os.ReadFile(path); return strings.Count(string(data), "\n") >= 2 })
	if string(data) != "first\nsecond\n" {
		t.Fatalf("mixed input lines: %q", data)
	}
}

func TestCanceledInputReservationDoesNotPassPriorSubmission(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "prompt-cancel")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	first := reserveTestInput(t, p, 0)
	if !first.Wait(context.Background()) {
		t.Fatal("first turn unavailable")
	}
	second := reserveTestInput(t, p, 0)
	third := reserveTestInput(t, p, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if second.Wait(ctx) {
		t.Fatal("canceled turn admitted")
	}
	ready := make(chan bool, 1)
	go func() { ready <- third.Wait(context.Background()) }()
	select {
	case <-ready:
		t.Fatal("third turn bypassed first")
	default:
	}
	first.Release()
	if !<-ready {
		t.Fatal("third turn canceled")
	}
	third.Release()
}

func TestDelayedInputDoesNotSubmitHumanInterruption(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "line")
	ready := filepath.Join(t.TempDir(), "ready")
	script := filepath.Join(t.TempDir(), "shell")
	body := fmt.Sprintf("#!/bin/sh\nstty echo; touch %q; IFS= read -r line; printf '%%s\\n' \"$line\" > %q\n", ready, path)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", script)
	s := newTestServer(t, "prompt-interrupt")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	defer s.sup.KillAll()
	p := s.sup.Get(s.app.State().Workspaces[0].Tabs[0].Layout.Focused())
	waitPromptCondition(t, func() bool { _, err := os.Stat(ready); return err == nil })
	keys, bad := parseKeys([]string{"Enter"})
	if bad != "" {
		t.Fatal(bad)
	}
	s.sendInputToPane(p, "first", keys)
	waitPromptCondition(t, func() bool { return strings.Contains(p.ScreenText(), "first") })
	s.sendInputToPane(p, "second", keys)
	p.RecordKeyInput()
	if _, err := p.WriteToPTY([]byte("human")); err != nil {
		t.Fatal(err)
	}
	<-time.After(700 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("interrupted input submitted: %v", err)
	}
	if _, err := p.WriteToPTY([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	var data []byte
	waitPromptCondition(t, func() bool { data, _ = os.ReadFile(path); return len(data) != 0 })
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "firsthuman\n" {
		t.Fatalf("input: %q %v", data, err)
	}
}
