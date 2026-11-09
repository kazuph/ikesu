package e2e

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// Uses a real server handoff and Git post-checkout hook, which can take over five seconds.
func TestLiveHandoffRetriesAfterWorktreeCreate(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	repo, root := filepath.Join(t.TempDir(), "repo"), t.TempDir()
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=t@t", "commit", "--allow-empty", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
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
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ready > '"+readyPath+"'\nread x < '"+gatePath+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := start(t)
	s.waitFor("shell ready", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	branch, checkout := "handoff-creation", filepath.Join(root, "checkout")
	if err := api.WriteRequest(conn, api.Request{ID: "create", Method: api.MethodWorktreeCreate, Params: api.MarshalParams(api.WorktreeCreateParams{Cwd: &repo, Branch: &branch, Path: &checkout})}); err != nil {
		t.Fatal(err)
	}
	ready, err := os.Open(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(ready).ReadString('y')
	ready.Close()
	if err != nil {
		t.Fatal(err)
	}
	output, handoffErr := s.cli("server", "live-handoff", "--import-exe", binPath)
	_, _ = gate.WriteString("go\n")
	if handoffErr == nil || !strings.Contains(output, "worktree operation in progress") {
		t.Fatalf("handoff did not reject active create: %s %v", output, handoffErr)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.Unmarshal(line, &response); err != nil || response.Err != nil {
		t.Fatalf("create response: %s %v", line, err)
	}
	var created api.WorktreeCreatedResult
	if err := json.Unmarshal(response.Result, &created); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(checkout); err != nil {
		t.Fatal(err)
	}
	if output, err = s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatalf("handoff retry: %s %v", output, err)
	}
	var workspaces api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &workspaces)
	found := false
	for _, w := range workspaces.Workspaces {
		if w.WorkspaceID == created.Workspace.WorkspaceID && w.Worktree != nil && w.Worktree.CheckoutPath == checkout {
			found = true
		}
	}
	if !found {
		t.Fatalf("created workspace missing after handoff: %+v", workspaces)
	}
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	found = false
	for _, p := range panes.Panes {
		if p.PaneID == created.RootPane.PaneID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created pane missing after handoff: %+v", panes)
	}
}

// Uses a real checkout removal and a real server handoff.
func TestLiveHandoffRetriesAfterWorktreeRemoval(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	repo, root := filepath.Join(t.TempDir(), "repo"), t.TempDir()
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("tracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "tracked"}, {"-c", "user.name=Test", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	s := start(t)
	s.waitFor("shell ready", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	branch, checkout := "handoff-removal", filepath.Join(root, "checkout")
	var created api.WorktreeCreatedResult
	s.rpc(api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &repo, Branch: &branch, Path: &checkout}, &created)
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
	defer func() { _, _ = gate.WriteString("go\n") }()
	hook, released := filepath.Join(root, "hook"), filepath.Join(root, "released")
	script := "#!/bin/sh\nif [ ! -f '" + released + "' ]; then\nprintf ready > '" + readyPath + "'\nread x < '" + gatePath + "'\n: > '" + released + "'\nfi\nprintf '\\0'\n"
	if err := os.WriteFile(hook, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "core.fsmonitor", hook).CombinedOutput(); err != nil {
		t.Fatalf("git config: %s %v", out, err)
	}
	conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{ID: "remove", Method: api.MethodWorktreeRemove, Params: api.MarshalParams(api.WorktreeRemoveParams{WorkspaceID: created.Workspace.WorkspaceID})}); err != nil {
		t.Fatal(err)
	}
	ready, err := os.Open(readyPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bufio.NewReader(ready).ReadString('y')
	ready.Close()
	if err != nil {
		t.Fatal(err)
	}
	output, handoffErr := s.cli("server", "live-handoff", "--import-exe", binPath)
	_, _ = gate.WriteString("go\n")
	if handoffErr == nil || !strings.Contains(output, "worktree operation in progress") {
		t.Fatalf("handoff did not reject active removal: %s %v", output, handoffErr)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response api.Response
	if err := json.Unmarshal(line, &response); err != nil || response.Err != nil {
		t.Fatalf("remove response: %s %v", line, err)
	}
	if _, err := os.Stat(checkout); !os.IsNotExist(err) {
		t.Fatalf("checkout remains: %v", err)
	}
	if output, err = s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatalf("handoff retry: %s %v", output, err)
	}
	var workspaces api.WorkspaceListResult
	s.rpc(api.MethodWorkspaceList, api.EmptyParams{}, &workspaces)
	for _, w := range workspaces.Workspaces {
		if w.WorkspaceID == created.Workspace.WorkspaceID {
			t.Fatalf("removed workspace returned: %+v", w)
		}
	}
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	for _, p := range panes.Panes {
		if p.PaneID == created.RootPane.PaneID {
			t.Fatalf("removed pane returned: %+v", p)
		}
	}
}
