package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	started, startErr := os.ReadFile(count)
	t.Logf("first timeout: shell starts=%q readError=%v", started, startErr)
	if retry := s.WaitLoginPath(); retry != err {
		t.Fatal("failure must be returned during cooldown")
	}
	// The brief proposes five seconds of failure cooldown, without a setting.
	time.Sleep(5*time.Second + time.Millisecond)
	var retryWorkers sync.WaitGroup
	retryErrors := make(chan error, 8)
	for n := 0; n < 8; n++ {
		retryWorkers.Add(1)
		go func() { defer retryWorkers.Done(); retryErrors <- s.WaitLoginPath() }()
	}
	retryWorkers.Wait()
	close(retryErrors)
	for err := range retryErrors {
		if err != nil {
			started, startErr := os.ReadFile(count)
			t.Fatalf("lookup did not recover: %v; shell starts=%q readError=%v", err, started, startErr)
		}
	}
	if _, err := s.SpawnAgent(1, dir, 80, 24, nil, []string{"/bin/sleep", "60"}); err != nil {
		t.Fatal("agent could not start after recovery", err)
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
	count := filepath.Join(filepath.Dir(shell), "queries")
	script, err := os.ReadFile(shell)
	if err != nil {
		t.Fatal(err)
	}
	script = []byte(strings.Replace(string(script), "#!/bin/sh\n", "#!/bin/sh\nprintf x >> '"+count+"'\n", 1))
	if err := os.WriteFile(shell, script, 0700); err != nil {
		t.Fatal(err)
	}
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
	if data, _ := os.ReadFile(count); string(data) != "x" {
		t.Fatalf("concurrent lookup count=%q", data)
	}
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoginPathRecoversAfterExitFailureAndEmptyPATH(t *testing.T) {
	teststate.Lock(t)
	for _, failure := range []string{"exit 1", "printf '\\000\\000'; exit 0"} {
		func() {
			dir := t.TempDir()
			shell := filepath.Join(dir, "login-shell")
			first := filepath.Join(dir, "first")
			count := filepath.Join(dir, "count")
			script := "#!/bin/sh\nprintf x >> '" + count + "'\nif [ ! -f '" + first + "' ]; then printf x > '" + first + "'; " + failure + "; fi\nprintf '\\000/usr/bin:/bin\\000'\n"
			if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			teststate.Setenv(t, "SHELL", shell)
			s := NewSupervisor()
			defer s.KillAll()
			err := s.WaitLoginPath()
			if err == nil {
				t.Fatal("invalid PATH lookup accepted")
			}
			if retry := s.WaitLoginPath(); retry != err {
				t.Fatal("failure cooldown not retained")
			}
			time.Sleep(5*time.Second + time.Millisecond)
			if err := s.WaitLoginPath(); err != nil {
				t.Fatal("lookup did not recover", err)
			}
			if data, _ := os.ReadFile(count); string(data) != "xx" {
				t.Fatalf("lookup count=%q", data)
			}
		}()
	}
}
