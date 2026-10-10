package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestConfigRestoreEnabledAndCanonicalCommandActualPTY(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := newTestServer(t, "config-auto")
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	on, off := true, false
	delay := 3000 // The user's configured restore delay.
	s.cfg.Session.ResumeAgentsOnRestore = &off
	s.cfg.AgentRestore.Enabled = &off
	s.cfg.AgentRestore.RestoreDelayMs = &delay
	s.cfg.AgentRestore.Commands = map[string]string{"claude": "/bin/echo {session_id}"}
	s.app.RecordSession(1, tui.AgentRecord{Agent: "claude", Kind: "id", Value: "config-auto", Source: "restore"})
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done; s.sup.KillAll() }()
	read := func() string {
		var screen api.PaneReadResultEnvelope
		s.app.Do(func() {
			resp := s.paneRead("read", api.PaneReadParams{PaneID: "p1", StripANSI: true})
			if err := json.Unmarshal(resp.Result, &screen); err != nil {
				t.Error(err)
			}
		})
		return screen.Read.Text
	}
	s.scheduleAutoResume()
	s.app.Do(func() {})
	if strings.Contains(read(), "config-auto") {
		t.Fatal("disabled restore launched")
	}
	s.app.Do(func() { s.cfg.AgentRestore.Enabled = &on })
	started := time.Now()
	s.scheduleAutoResume()
	deadline := started.Add(5 * time.Second)
	for {
		text := read()
		if strings.Contains(text, "\nconfig-auto") {
			if time.Since(started) < time.Duration(delay)*time.Millisecond || !strings.Contains(strings.ReplaceAll(text, "\n", ""), "/bin/echo config-auto") {
				t.Fatal("delay or canonical command was not applied", text)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("enabled restore did not execute canonical command", text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
