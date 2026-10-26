package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLoginPathRecoversAfterTimeoutAndCachesSuccess(t *testing.T) {
	teststate.Lock(t)
	old := loginShellPATHTimeout
	loginShellPATHTimeout = 500 * time.Millisecond
	t.Cleanup(func() { loginShellPATHTimeout = old })
	dir := t.TempDir()
	shell := filepath.Join(dir, "login-shell")
	count := filepath.Join(dir, "count")
	first := filepath.Join(dir, "first")
	script := "#!/bin/sh\nprintf x >> '" + count + "'\nif [ ! -f '" + first + "' ]; then printf x > '" + first + "'; exec /bin/sleep 60; fi\nprintf '\\000/usr/bin:/bin\\000'\n"
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	err := s.WaitLoginPath()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("first lookup did not time out", err)
	}
	if retry := s.WaitLoginPath(); retry != err {
		t.Fatal("failure must be returned during cooldown")
	}
	// The brief proposes five seconds of failure cooldown, without a setting.
	time.Sleep(5*time.Second + time.Millisecond)
	if err := s.WaitLoginPath(); err != nil {
		t.Fatal("lookup did not recover", err)
	}
	for n := 0; n < 3; n++ {
		if path, err := s.LoginPath(); err != nil || path != "/usr/bin:/bin" {
			t.Fatal(path, err)
		}
	}
	if data, _ := os.ReadFile(count); string(data) != "xx" {
		t.Fatalf("lookup count=%q", data)
	}
}

func TestLoginPathConcurrentRequestsShareLookupAndAppLoopDoesNotWait(t *testing.T) {
	teststate.Lock(t)
	shell, started, release := teststate.GatedLoginShell(t)
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	var workers sync.WaitGroup
	failures := make(chan error, 8)
	for n := 0; n < 8; n++ {
		workers.Add(1)
		go func() { defer workers.Done(); failures <- s.WaitLoginPath() }()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lookup not started")
		}
		time.Sleep(time.Millisecond)
	}
	before := time.Now()
	if _, err := s.LoginPath(); !errors.Is(err, errLoginPathPending) {
		t.Fatal(err)
	}
	if time.Since(before) > time.Second {
		t.Fatal("app-loop lookup waited")
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
}
