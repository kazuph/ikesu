package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

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
	first := p.ReserveInputOperation()
	if !first.Wait(context.Background()) {
		t.Fatal("first turn unavailable")
	}
	second := p.ReserveInputOperation()
	third := p.ReserveInputOperation()
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
	waitPromptCondition(t, func() bool { _, err := os.Stat(path); return err == nil })
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "firsthuman\n" {
		t.Fatalf("input: %q %v", data, err)
	}
}
