package runtime

import (
	"context"
	"errors"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview1LoginPATHTimeoutFailsClosed(t *testing.T) {
	teststate.Lock(t)
	old := loginShellPATHTimeout
	loginShellPATHTimeout = 500 * time.Millisecond
	t.Cleanup(func() { loginShellPATHTimeout = old })
	dir, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	shell := filepath.Join(dir, "slowsh")
	count := filepath.Join(dir, "count")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf x >> '"+count+"'\n/bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	before := time.Now()
	err = s.WaitLoginPath()
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(before) < loginShellPATHTimeout || time.Since(before) > time.Second {
		t.Fatal(err, time.Since(before))
	}
	if retry := s.WaitLoginPath(); retry != err {
		t.Fatal("failure not cached", retry, err)
	}
	// A deadline may cancel before exec begins; either outcome must never retry.
	b, readErr := os.ReadFile(count)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(b) > 1 {
		t.Fatal("lookup repeated", string(b))
	}
	t.Logf("deadline=%s elapsed=%s shell starts=%q", loginShellPATHTimeout, time.Since(before), b)
	if p, err := s.SpawnAgent(1, dir, 80, 24, nil, []string{"/bin/echo", "must not launch"}); err == nil || p != nil || s.Get(1) != nil {
		t.Fatal(p, err)
	}
}

func TestReview1ExplicitAgentPATHAndOrdinaryArgv(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	physicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	dir = physicalDir
	login := filepath.Join(dir, "login")
	server := filepath.Join(dir, "server")
	for _, d := range []string{login, server} {
		if err := os.Mkdir(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ dir, label string }{{login, "LOGIN"}, {server, "SERVER"}} {
		script := "#!/bin/sh\nprintf '" + tc.label + " PATH=%s ARG=%s CWD=%s\\n' \"$PATH\" \"$1\" \"$PWD\"\nexec /bin/sleep 60\n"
		if err := os.WriteFile(filepath.Join(tc.dir, "review-agent"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	shell := filepath.Join(dir, "loginsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '\\000%s\\000' '"+login+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "PATH", server)
	s := NewSupervisor()
	defer s.KillAll()
	if err := s.WaitLoginPath(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		agent       bool
		path, label string
	}{{true, "/x", "LOGIN"}, {false, "/ordinary", "SERVER"}} {
		spawn := s.SpawnWith
		if tc.agent {
			spawn = s.SpawnAgent
		}
		id := 1
		if !tc.agent {
			id = 2
		}
		p, err := spawn(id, dir, 240, 24, []string{"PATH=" + tc.path}, []string{"review-agent", "argument with spaces"})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		want := tc.label + " PATH=" + tc.path + " ARG=argument with spaces CWD=" + dir
		for {
			var text string
			p.WithEmu(func(e *vt.Emulator) { text = e.String() })
			if strings.Contains(text, want) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(text, want)
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestReview1OrdinaryArgvDoesNotReadLoginShell(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	shell := filepath.Join(dir, "failsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf x >> '"+count+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	if _, err := s.SpawnWith(1, dir, 80, 24, nil, []string{"/bin/sleep", "60"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(count); !os.IsNotExist(err) {
		t.Fatal("ordinary argv queried login shell", err)
	}
}

func TestReview1LoginPATHDeadlineIncludesInheritedOutput(t *testing.T) {
	teststate.Lock(t)
	old := loginShellPATHTimeout
	loginShellPATHTimeout = 500 * time.Millisecond
	t.Cleanup(func() { loginShellPATHTimeout = old })
	dir, err := os.MkdirTemp("/tmp", "gtxr1-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	shell := filepath.Join(dir, "holdstdout")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n/bin/sleep 60 &\nprintf '\\000%s\\000' \"$PATH\"\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	before := time.Now()
	if err := s.WaitLoginPath(); !errors.Is(err, context.DeadlineExceeded) || time.Since(before) > time.Second {
		t.Fatal(err, time.Since(before))
	}
	if p, err := s.SpawnAgent(1, dir, 80, 24, nil, []string{"/bin/sleep", "60"}); err == nil || p != nil {
		t.Fatal("partial PATH used", p, err)
	}
}
