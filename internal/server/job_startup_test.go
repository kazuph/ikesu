package server

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/childenv"
	"golang.org/x/sys/unix"
)

func TestSupervisorStartupDisconnectAbortsRealChild(t *testing.T) {
	for _, phase := range []string{"before-request", "before-commit", "before-release"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "job.log")
			fifo := filepath.Join(dir, "gate")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			gate, err := os.OpenFile(fifo, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Close()
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
			if err != nil {
				t.Fatal(err)
			}
			a, b := os.NewFile(uintptr(pair[0]), "test-parent"), os.NewFile(uintptr(pair[1]), "test-child")
			conn, err := net.FileConn(a)
			a.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := childenv.Command(exe, jobSupervisorArg)
			cmd.ExtraFiles = []*os.File{b}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			b.Close()
			enc, dec := gob.NewEncoder(conn), gob.NewDecoder(conn)
			conn.SetDeadline(time.Now().Add(handoffWait))
			var started jobRunnerStarted
			if phase != "before-request" {
				req := jobRunnerRequest{JobID: "startup", Token: "startup-identity", LogPath: log, Cwd: dir, Namespace: "ikesu", Argv: []string{"/bin/sh", "-c", fmt.Sprintf("read value < %q", fifo)}}
				if err = enc.Encode(req); err != nil {
					t.Fatal(err)
				}
				if err = dec.Decode(&started); err != nil || started.Error != "" {
					t.Fatal(started, err)
				}
				if phase == "before-release" {
					if err = enc.Encode(jobRunnerCommit{StartedMs: time.Now().UnixMilli()}); err != nil {
						t.Fatal(err)
					}
					if err = dec.Decode(&started); err != nil || started.Error != "" {
						t.Fatal(started, err)
					}
				}
			}
			conn.Close()
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("startup disconnect reported success")
				}
			case <-time.After(handoffWait):
				t.Fatal("startup ownership did not drain")
			}
			if started.PID > 0 && processGroupAlive(started.PID) {
				t.Fatal("uncommitted child remains", started.PID)
			}
			if phase != "before-request" {
				evidence, err := os.ReadFile(log + ".monitor-exit.json")
				if err != nil || !strings.Contains(string(evidence), "startup-identity") {
					t.Fatal("startup evidence missing", string(evidence), err)
				}
			}
		})
	}
}
