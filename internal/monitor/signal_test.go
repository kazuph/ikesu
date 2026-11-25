package monitor

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRunawaySignalBoundRejectsUnsafeTargetWithoutSending(t *testing.T) {
	t.Parallel()
	shell := Process{Identity: Identity{10, "shell"}, Parent: 1, Group: 10}
	child := Process{Identity: Identity{20, "child"}, Parent: 10, Group: 20}
	target := Target{Identity: child.Identity, Path: "/bin/test"}
	roots := Roots{1: shell.Identity}
	for _, tc := range []struct {
		name  string
		sid   int
		noSID bool
		path  string
		want  error
	}{
		{name: "same pane session", sid: 10, path: target.Path},
		{name: "another session", sid: 20, path: target.Path, want: errBinding},
		{name: "unbound session or group", noSID: true, path: target.Path, want: errBinding},
		{name: "exec changed", sid: 10, path: "/bin/changed", want: errExecutable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sends := 0
			read := func(pid int) (Process, error) {
				switch pid {
				case shell.PID:
					return shell, nil
				case child.PID:
					return child, nil
				default:
					return Process{}, unix.ESRCH
				}
			}
			getSID := func(pid int) (int, error) {
				if pid == shell.PID {
					return shell.PID, nil
				}
				if tc.noSID {
					return 0, unix.ESRCH
				}
				return tc.sid, nil
			}
			err := signalBound(target, shell.Identity, roots, unix.SIGTERM, read, func(int) (string, error) {
				return tc.path, nil
			}, getSID, func(pid int, sig unix.Signal) error {
				if pid != child.PID || sig != unix.SIGTERM {
					t.Fatalf("sent to %d with %v", pid, sig)
				}
				sends++
				return nil
			})
			if !errors.Is(err, tc.want) || (tc.want != nil && sends != 0) || (tc.want == nil && sends != 1) {
				t.Fatalf("error %v, sends %d", err, sends)
			}
		})
	}
}
