package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

func TestDisconnectedAPIRequestDoesNotRunAfterAppResumes(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "disconnected-api")
	dir, err := os.MkdirTemp("/tmp", "ikesu-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	blocked := make(chan struct{})
	release := make(chan struct{})
	go s.app.Do(func() { close(blocked); <-release })
	<-blocked
	label := "must-not-appear"
	requests := []api.Request{
		{ID: "test", Method: api.MethodPaneRename, Params: api.MarshalParams(api.PaneRenameParams{PaneID: "p1", Label: &label})},
		{ID: "read", Method: api.MethodPaneList},
		{ID: "prompt", Method: api.MethodAgentPrompt, Params: api.MarshalParams(api.AgentPromptParams{Target: "p1", Text: "do not run"})},
		{ID: "output", Method: api.MethodPaneWaitForOutput, Params: api.MarshalParams(api.PaneWaitForOutputParams{PaneID: "p1", Match: api.OutputMatch{Type: "substring", Value: "missing"}})},
	}
	served := make(chan struct{}, len(requests))
	go func() {
		for range requests {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.apiWG.Add(1)
			go func() { s.serveAPI(conn); served <- struct{}{} }()
		}
	}()
	var connections []net.Conn
	for _, req := range requests {
		conn, err := net.Dial("unix", ln.Addr().String())
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		connections = append(connections, conn)
		if err := api.WriteRequest(conn, req); err != nil {
			close(release)
			t.Fatal(err)
		}
	}
	<-time.After(100 * time.Millisecond)
	for _, conn := range connections {
		conn.Close()
	}
	var retained bool
	completed := 0
	deadline := time.After(300 * time.Millisecond)
	for completed < len(requests) {
		select {
		case <-served:
			completed++
		case <-deadline:
			retained = true
			goto unblocked
		}
	}
unblocked:
	close(release)
	for completed < len(requests) {
		<-served
		completed++
	}
	var got string
	s.app.Do(func() { got = s.app.State().PaneLabels[1] })
	if retained || got == label {
		t.Fatalf("disconnected request retained=%v label=%q", retained, got)
	}
}

func TestDisconnectedAPIWhileGitHookBlocked(t *testing.T) {
	teststate.Lock(t)
	dir, err := os.MkdirTemp("/tmp", "ikesu-git-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	runGit("init")
	runGit("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	fifo, started := filepath.Join(dir, "release"), filepath.Join(dir, "started")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	hook := fmt.Sprintf("#!/bin/sh\ntouch %q\nread x < %q\n", started, fifo)
	if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-checkout"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s := newTestServer(t, "git-blocked")
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	gitCtx, cancelGit := context.WithCancel(context.Background())
	defer cancelGit()
	gitDone := make(chan error, 1)
	target := filepath.Join(repo, ".worktree", "test")
	go s.app.Do(func() {
		cmd := exec.CommandContext(gitCtx, "git", "-C", repo, "worktree", "add", "-b", "test-branch", target, "HEAD")
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		gitDone <- cmd.Run()
	})
	waitPromptCondition(t, func() bool { _, err := os.Stat(started); return err == nil })
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := os.WriteFile(fifo, []byte("release\n"), 0600); err != nil {
				t.Error(err)
			}
		})
	}
	defer release()
	served := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			s.apiWG.Add(1)
			s.serveAPI(conn)
		}
		close(served)
	}()
	conn, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	label := "must-not-run"
	if err := api.WriteRequest(conn, api.Request{ID: "test", Method: api.MethodPaneRename, Params: api.MarshalParams(api.PaneRenameParams{PaneID: "p1", Label: &label})}); err != nil {
		t.Fatal(err)
	}
	<-time.After(100 * time.Millisecond)
	conn.Close()
	retained := false
	select {
	case <-served:
	case <-time.After(300 * time.Millisecond):
		retained = true
	}
	release()
	if err := <-gitDone; err != nil {
		t.Fatalf("started git operation did not complete: %v", err)
	}
	<-served
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("started operation is not visible: %v", err)
	}
	var got string
	s.app.Do(func() { got = s.app.State().PaneLabels[1] })
	if retained || got == label {
		t.Fatalf("disconnected request retained=%v label=%q", retained, got)
	}
}

func TestAPIHalfClosedWriteStillReceivesResponse(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "api-half-close")
	dir, err := os.MkdirTemp("/tmp", "ikesu-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	served := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			s.apiWG.Add(1)
			s.serveAPI(conn)
		}
		close(served)
	}()
	conn, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{ID: "test", Method: api.MethodPaneList}); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil || resp.Err != nil {
		t.Fatalf("half-close response: %+v %v", resp, err)
	}
	<-served
}
