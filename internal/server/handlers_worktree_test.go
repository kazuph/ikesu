package server

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/cli"
)

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
func TestWorktreeSocketCLIAndEvents(t *testing.T) {
	r := gitRepo(t)
	t.Setenv("HOME", t.TempDir())
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	var listed api.WorktreeListResult
	apiOK(t, dir, api.MethodWorktreeList, api.WorktreeListParams{Cwd: &r}, &listed)
	if len(listed.Worktrees) != 1 || listed.Source.RepoKey == "" {
		t.Fatalf("%+v", listed)
	}
	branch := "socket/new"
	path := filepath.Join(t.TempDir(), "checkout")
	var created api.WorktreeCreatedResult
	apiOK(t, dir, api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &r, Path: &path, Branch: &branch, Focus: true}, &created)
	if !created.Workspace.Focused || created.Workspace.Worktree == nil || created.Worktree.Branch == nil || *created.Worktree.Branch != branch {
		t.Fatalf("%+v", created)
	}
	var opened api.WorktreeOpenedResult
	apiOK(t, dir, api.MethodWorktreeOpen, api.WorktreeOpenParams{Cwd: &r, Branch: &branch, Focus: true}, &opened)
	if !opened.AlreadyOpen || opened.Workspace.WorkspaceID != created.Workspace.WorkspaceID {
		t.Fatal("duplicate open")
	}
	// The CLI reaches the real server socket, with a temporary real Git repo.
	var output strings.Builder
	code := cli.Run([]string{"worktree", "create", "--cwd", r, "--branch", "existing", "--path", filepath.Join(t.TempDir(), "existing"), "--json"}, cli.Options{Session: "test", Explicit: true, Stdout: &output, Stderr: &output})
	if code != 0 {
		t.Fatal(output.String())
	}
	var response api.Response
	if e := json.Unmarshal([]byte(output.String()), &response); e != nil {
		t.Fatal(e)
	}
	var existing api.WorktreeCreatedResult
	if e := json.Unmarshal(response.Result, &existing); e != nil || existing.Worktree.Branch == nil || *existing.Worktree.Branch != "existing" {
		t.Fatalf("%+v %v", existing, e)
	}
	if e := os.WriteFile(filepath.Join(path, "unsaved"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if resp := apiReq(t, dir, api.MethodWorktreeRemove, api.WorktreeRemoveParams{WorkspaceID: created.Workspace.WorkspaceID}); resp.Err == nil {
		t.Fatal("dirty remove succeeded")
	}
	var ws api.WorkspaceInfoResult
	apiOK(t, dir, api.MethodWorkspaceGet, api.WorkspaceTarget{WorkspaceID: created.Workspace.WorkspaceID}, &ws)
	if !ws.Workspace.Focused {
		t.Fatal("failed remove changed focus")
	}
	apiOK(t, dir, "workspace.move", map[string]any{"workspace_id": existing.Workspace.WorkspaceID, "insert_index": 0}, nil)
	var removed api.WorktreeRemovedResult
	apiOK(t, dir, api.MethodWorktreeRemove, api.WorktreeRemoveParams{WorkspaceID: created.Workspace.WorkspaceID, Force: true}, &removed)
	if !removed.Forced || removed.Path != created.Worktree.Path {
		t.Fatalf("%+v", removed)
	}
	var workspaces api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &workspaces)
	focused := 0
	for _, w := range workspaces.Workspaces {
		if w.Focused {
			focused++
		}
	}
	if focused != 1 {
		t.Fatal("focus invalid after delete")
	}
	// Journal replay proves the schema-defined event payloads were emitted.
	conn, rd := subscribe(t, dir, []string{"worktree.created", "worktree.opened", "worktree.removed", "workspace.moved"})
	defer conn.Close()
	seen := map[string]bool{}
	for len(seen) < 4 {
		var msg map[string]json.RawMessage
		line, e := rd.ReadBytes('\n')
		if e != nil {
			t.Fatal(e)
		}
		if e := json.Unmarshal(line, &msg); e != nil {
			t.Fatal(e)
		}
		var kind string
		_ = json.Unmarshal(msg["event"], &kind)
		if kind != "" {
			seen[kind] = true
		}
	}
	for _, kind := range []string{"worktree_created", "worktree_opened", "worktree_removed", "workspace_moved"} {
		if !seen[kind] {
			t.Fatalf("event missing %s: %v", kind, seen)
		}
	}
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

func TestWorktreeCLIAllOperationsAndBareSocket(t *testing.T) {
	r := gitRepo(t)
	t.Setenv("HOME", t.TempDir())
	bare := filepath.Join(t.TempDir(), "bare.git")
	if b, e := exec.Command("git", "clone", "--bare", r, bare).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	requestCLI := func(args ...string) api.Response {
		t.Helper()
		var output strings.Builder
		if code := cli.Run(append([]string{"worktree"}, args...), cli.Options{Session: "test", Explicit: true, Stdout: &output, Stderr: &output}); code != 0 {
			t.Fatal(output.String())
		}
		var resp api.Response
		if e := json.Unmarshal([]byte(output.String()), &resp); e != nil {
			t.Fatal(e)
		}
		return resp
	}
	path := filepath.Join(t.TempDir(), "cli-new")
	resp := requestCLI("create", "--cwd", r, "--branch", "cli/new", "--path", path, "--focus", "--json")
	var created api.WorktreeCreatedResult
	if e := json.Unmarshal(resp.Result, &created); e != nil {
		t.Fatal(e)
	}
	if created.Workspace.WorkspaceID == "" || created.Worktree.Branch == nil || *created.Worktree.Branch != "cli/new" {
		t.Fatalf("%+v", created)
	}
	requestCLI("list", "--cwd", r, "--json")
	resp = requestCLI("open", "--cwd", r, "--branch", "cli/new", "--json")
	var opened api.WorktreeOpenedResult
	_ = json.Unmarshal(resp.Result, &opened)
	if !opened.AlreadyOpen {
		t.Fatal("CLI open duplicated")
	}
	requestCLI("remove", "--workspace", created.Workspace.WorkspaceID, "--json")
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("CLI delete left directory")
	}
	var bareCreated api.WorktreeCreatedResult
	branch := "bare/socket"
	barePath := filepath.Join(t.TempDir(), "bare-linked")
	apiOK(t, dir, api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &bare, Branch: &branch, Path: &barePath}, &bareCreated)
	if bareCreated.Workspace.Worktree == nil || bareCreated.Workspace.Worktree.RepoName != "bare" {
		t.Fatalf("%+v", bareCreated)
	}
	var list api.WorktreeListResult
	apiOK(t, dir, api.MethodWorktreeList, api.WorktreeListParams{Cwd: &bare}, &list)
	if len(list.Worktrees) != 2 || !list.Worktrees[0].IsBare {
		t.Fatalf("%+v", list)
	}
	apiOK(t, dir, api.MethodWorktreeRemove, api.WorktreeRemoveParams{WorkspaceID: bareCreated.Workspace.WorkspaceID}, nil)
	// The bare directory itself is never a deletable checkout.
	resp = apiReq(t, dir, api.MethodWorktreeOpen, api.WorktreeOpenParams{Cwd: &bare, Path: &bare})
	if resp.Err == nil {
		t.Fatal("opened bare repository as checkout")
	}
}
