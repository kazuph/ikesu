package runtime

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestG9LoginShellPATHCacheArgumentsAndCwd(t *testing.T) {
	teststate.Lock(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(dir, "login-shell")
	count := filepath.Join(dir, "queries")
	result := filepath.Join(dir, "result")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf x >> '"+count+"'\nprintf '\\000%s\\000' '"+bin+":/usr/bin:/bin'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "temporary-agent"), []byte("#!/bin/sh\nprintf 'CWD=%s ARG=%s PATH=%s\\n' \"$PWD\" \"$1\" \"$PATH\" > '"+result+"'\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "PATH", "/usr/bin:/bin")
	s := NewSupervisor()
	defer s.KillAll()
	if err := s.WaitLoginPath(); err != nil {
		t.Fatal(err)
	}
	argv := []string{"temporary-agent", "argument with spaces"}
	for id := 1; id <= 2; id++ {
		p, err := s.SpawnAgent(id, dir, 240, 10, nil, argv)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		want := "CWD=" + dir + " ARG=argument with spaces PATH=" + bin + ":/usr/bin:/bin\n"
		for {
			data, err := os.ReadFile(result)
			if err == nil && string(data) == want {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal(string(data), err, want)
			}
			time.Sleep(time.Millisecond)
		}

		if !reflect.DeepEqual(p.SpawnArgv(), argv) {
			t.Fatal(p.SpawnArgv())
		}
	}
	data, err := os.ReadFile(count)
	if err != nil || string(data) != "x" {
		t.Fatal("PATH was not cached", string(data), err)
	}
}

func TestG9LoginShellFailureDoesNotGuessPATH(t *testing.T) {
	teststate.Lock(t)
	shell := filepath.Join(t.TempDir(), "failed-shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	s := NewSupervisor()
	defer s.KillAll()
	if err := s.WaitLoginPath(); err == nil {
		t.Fatal("expected shell failure")
	}
	if p, err := s.SpawnAgent(1, t.TempDir(), 80, 24, nil, []string{"/bin/echo", "should not start"}); err == nil || p != nil || s.Get(1) != nil {
		t.Fatal("shell failure bypassed", p, err)
	}
}
