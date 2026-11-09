package server

import (
	"bufio"
	"encoding/json"
	"fmt"

	"net"
	"os"
	"path/filepath"
	"syscall"

	"os/exec"

	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestWorktreeCreateHookDoesNotBlockOtherAPI(t *testing.T) {
	teststate.Lock(t)
	repo := gitRepo(t)
	root := t.TempDir()
	fifo, ready := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(fifo, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ready > '"+ready+"'\nread x < '"+fifo+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dir, done := startTestServer(t)
	defer func() { _, _ = gate.WriteString("go\n"); stopServer(t, dir, done) }()
	paneID := firstPaneID(t, dir)
	branch, path := "isolated", filepath.Join(root, "checkout")
	conn, response := decisionStream(t, dir, api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &repo, Branch: &branch, Path: &path})
	defer conn.Close()
	until := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("post-checkout hook did not start")
		}
		time.Sleep(time.Millisecond)
	}
	other, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.SetDeadline(time.Now().Add(time.Second))
	if err := api.WriteRequest(other, api.Request{ID: "list", Method: api.MethodPaneList, Params: api.MarshalParams(api.PaneListParams{})}); err != nil {
		t.Fatal(err)
	}
	_, readErr := bufio.NewReader(other).ReadBytes('\n')
	if readErr == nil {
		marker := filepath.Join(root, "other-pane")
		other.SetDeadline(time.Now().Add(time.Second))
		err = api.WriteRequest(other, api.Request{ID: "input", Method: api.MethodPaneSendInput, Params: api.MarshalParams(api.PaneSendInputParams{PaneID: paneID, Text: "printf ready > '" + marker + "'", Keys: []string{"Enter"}})})
		if err == nil {
			_, err = bufio.NewReader(other).ReadBytes('\n')
		}
		if err == nil {
			deadline := time.Now().Add(time.Second)
			for {
				if _, statErr := os.Stat(marker); statErr == nil {
					break
				}
				if time.Now().After(deadline) {
					err = fmt.Errorf("other pane input did not run")
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		if err != nil {
			readErr = err
		}
	}
	_, _ = gate.WriteString("go\n")
	if readErr != nil {
		t.Fatalf("pane list blocked while worktree hook waited: %v", readErr)
	}
	line, err := response.ReadBytes('\n')
	if err != nil || !strings.Contains(string(line), "worktree_created") {
		t.Fatalf("create result: %s %v", line, err)
	}
	var responseEnvelope api.Response
	var created api.WorktreeCreatedResult
	if err := json.Unmarshal(line, &responseEnvelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(responseEnvelope.Result, &created); err != nil {
		t.Fatal(err)
	}
	var listed api.WorktreeListResult
	apiOK(t, dir, api.MethodWorktreeList, api.WorktreeListParams{Cwd: &repo}, &listed)
	found := false
	for _, item := range listed.Worktrees {
		if item.Path == created.Worktree.Path {
			found = true
		}
	}
	if !found {
		t.Fatal("created checkout missing from list")
	}
	var removed api.WorktreeRemovedResult
	apiOK(t, dir, api.MethodWorktreeRemove, api.WorktreeRemoveParams{WorkspaceID: created.Workspace.WorkspaceID}, &removed)
	if removed.Path != created.Worktree.Path {
		t.Fatalf("removed path %q, want %q", removed.Path, created.Worktree.Path)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	r := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init"}, {"branch", "existing"}} {
		if b, e := exec.Command("git", append([]string{"-C", r}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	return r
}

func subscribe(t *testing.T, dir string, kinds []string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, e := net.Dial("unix", api.APISocketPath(dir))
	if e != nil {
		t.Fatal(e)
	}
	subs := []api.Subscription{}
	for _, k := range kinds {
		subs = append(subs, api.Subscription{Type: k})
	}
	if e := api.WriteRequest(conn, api.Request{ID: "wt-events", Method: api.MethodEventsSubscribe, Params: api.MarshalParams(api.EventsSubscribeParams{Subscriptions: subs})}); e != nil {
		t.Fatal(e)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(conn)
	line, e := rd.ReadBytes('\n')
	if e != nil || !strings.Contains(string(line), "subscription_started") {
		t.Fatalf("%s %v", line, e)
	}
	return conn, rd
}

func TestLiveHandoffRejectsWorktreeCreateInProgress(t *testing.T) {
	teststate.Lock(t)
	repo := gitRepo(t)
	root := t.TempDir()
	gatePath, readyPath := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(gatePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(readyPath, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ready > '"+readyPath+"'\nread x < '"+gatePath+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	dir, done := startTestServer(t)
	defer func() { _, _ = gate.WriteString("go\n"); stopServer(t, dir, done) }()
	branch, path := "handoff-create", filepath.Join(root, "checkout")
	conn, response := decisionStream(t, dir, api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &repo, Branch: &branch, Path: &path})
	defer conn.Close()
	ready, err := os.Open(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(ready).ReadString('y')
	ready.Close()
	if err != nil {
		t.Fatal(err)
	}
	handoff, result := decisionStream(t, dir, api.MethodServerLiveHandoff, api.ServerLiveHandoffParams{})
	defer handoff.Close()
	line, err := result.ReadBytes('\n')
	_, _ = gate.WriteString("go\n")
	if err != nil || !strings.Contains(string(line), "worktree operation in progress") {
		t.Fatalf("handoff must reject pending worktree operation: %s %v", line, err)
	}
	line, err = response.ReadBytes('\n')
	if err != nil || !strings.Contains(string(line), "worktree_created") {
		t.Fatalf("creation did not complete: %s %v", line, err)
	}
}

func TestLiveHandoffRejectsWorktreeRemovalInProgress(t *testing.T) {
	teststate.Lock(t)
	repo := gitRepo(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("tracked"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "tracked"}, {"-c", "user.name=Test", "-c", "user.email=t@t", "commit", "-qm", "tracked"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	dir, done := startTestServer(t)
	branch, path := "handoff-remove", filepath.Join(root, "checkout")
	var created api.WorktreeCreatedResult
	apiOK(t, dir, api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &repo, Branch: &branch, Path: &path}, &created)
	gatePath, readyPath := filepath.Join(root, "gate"), filepath.Join(root, "ready")
	if err := syscall.Mkfifo(gatePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(readyPath, 0600); err != nil {
		t.Fatal(err)
	}
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	defer func() { _, _ = gate.WriteString("go\n"); stopServer(t, dir, done) }()
	released, hook := filepath.Join(root, "released"), filepath.Join(root, "fsmonitor")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nif [ ! -f '"+released+"' ]; then\nprintf ready > '"+readyPath+"'\nread x < '"+gatePath+"'\n: > '"+released+"'\nfi\nprintf '\\0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "core.fsmonitor", hook).CombinedOutput(); err != nil {
		t.Fatalf("git config: %s %v", out, err)
	}
	conn, response := decisionStream(t, dir, api.MethodWorktreeRemove, api.WorktreeRemoveParams{WorkspaceID: created.Workspace.WorkspaceID})
	defer conn.Close()
	ready, err := os.Open(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(ready).ReadString('y')
	ready.Close()
	if err != nil {
		t.Fatal(err)
	}
	handoff, result := decisionStream(t, dir, api.MethodServerLiveHandoff, api.ServerLiveHandoffParams{})
	defer handoff.Close()
	line, err := result.ReadBytes('\n')
	_, _ = gate.WriteString("go\n")
	if err != nil || !strings.Contains(string(line), "worktree operation in progress") {
		t.Fatalf("handoff must reject pending removal: %s %v", line, err)
	}
	line, err = response.ReadBytes('\n')
	if err != nil || !strings.Contains(string(line), "worktree_removed") {
		t.Fatalf("removal did not complete: %s %v", line, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkout remains after removal: %v", err)
	}
}
