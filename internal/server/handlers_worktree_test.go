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
