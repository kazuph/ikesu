package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/teststate"
	"golang.org/x/sys/unix"
)

// The wrapper runs the real command and only gates when it returns (du) or
// starts removal (git). The FIFOs place the competing operation exactly there.
type pruneToolGate struct {
	marker        string
	ready, resume *os.File
}

func newPruneToolGate(t *testing.T, dir, key string) pruneToolGate {
	t.Helper()
	open := func(suffix string) *os.File {
		path := filepath.Join(dir, key+suffix)
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		return file
	}
	gate := pruneToolGate{filepath.Join(dir, key+".armed"), open(".ready"), open(".resume")}
	// A failing assertion must let any waiting child reach its normal cleanup.
	t.Cleanup(func() { _, _ = gate.resume.WriteString("go\n") })
	return gate
}

func (g pruneToolGate) arm(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(g.marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func (g pruneToolGate) disarm(t *testing.T) {
	t.Helper()
	if err := os.Remove(g.marker); err != nil {
		t.Fatal(err)
	}
}

func (g pruneToolGate) wait(t *testing.T) int {
	t.Helper()
	type reply struct {
		pid int
		err error
	}
	done := make(chan reply, 1)
	go func() {
		line, err := bufio.NewReader(g.ready).ReadString('\n')
		if err != nil {
			done <- reply{err: err}
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		done <- reply{pid: pid, err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil || r.pid < 1 {
			t.Fatalf("gate process: %+v", r)
		}
		return r.pid
	case <-time.After(5 * time.Second):
		t.Fatalf("command did not arrive at %s", g.marker)
		return 0
	}
}

func (g pruneToolGate) release(t *testing.T) {
	t.Helper()
	g.disarm(t)
	if _, err := g.resume.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
}

func pruneGatedServerSetup(t *testing.T, dir string) func(string) string {
	t.Helper()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	prefix := "#!/bin/sh\nroot=" + quote(dir) + "\n"
	tools := map[string]string{
		"du": `key=du-${4##*/}
if [ -f "$root/$key.armed" ]; then
	"$real" "$@" || exit "$?"
	printf '%s\n' "$$" > "$root/$key.ready"
	read -r go < "$root/$key.resume"
	exit 0
fi
`,
		"git": `if [ "${3-}" = worktree ] && [ "${4-}" = remove ]; then
	key=git-${6##*/}
	if [ -f "$root/$key.armed" ]; then
		printf '%s\n' "$$" > "$root/$key.ready"
		read -r go < "$root/$key.resume"
		"$real" "$@" > "$root/$key.output" 2>&1
		result=$?
		cat "$root/$key.output"
		exit "$result"
	fi
fi
`,
	}
	for name, body := range tools {
		real, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(prefix+"real="+quote(real)+"\n"+body+"exec \"$real\" \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return func(string) string {
		teststate.Setenv(t, "PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		return ""
	}
}

func openPruneFixtureOnMachine(t *testing.T, h *rightMachineHarness, id string) string {
	t.Helper()
	return openPruneFixtureIn(t, h, id, t.TempDir())
}

func openPruneFixtureIn(t *testing.T, h *rightMachineHarness, id, checkoutParent string) string {
	t.Helper()
	root := t.TempDir()
	runPruneGit(t, root, "init", "-qb", "main")
	if err := os.Mkdir(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child", "tracked"), []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runPruneGit(t, root, "add", "child")
	runPruneGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "init")
	name := id + "-review"
	path := filepath.Join(checkoutParent, name)
	runPruneGit(t, root, "worktree", "add", "-qb", name, path)
	h.api(id, api.MethodWorktreeOpen, api.WorktreeOpenParams{Cwd: &root, Path: &path, Label: &name})
	h.until("linked worktree on "+id, func() bool { return len(h.m.endpoints[id].snapshot.State.Workspaces) == 3 })
	return path
}

func pruneTestKey(t *testing.T, m *machineClient, code rune) {
	t.Helper()
	if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: code}}); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRevalidatesAfterMeasurementAndReservesDuringRemoval(t *testing.T) {
	gates := t.TempDir()
	h := newRightMachineHarnessPeers(t, pruneGatedServerSetup(t, gates), []string{"local"}, true)
	measure := newPruneToolGate(t, gates, "du-local-review")
	remove := newPruneToolGate(t, gates, "git-local-review")
	m := h.m
	path := openPruneFixtureOnMachine(t, h, "local")
	scan := func() {
		t.Helper()
		m.openPruneWorkspaces()
		pruneTestKey(t, m, uv.KeyEnter)
		h.until("preview ready", func() bool { return len(m.prune.pending) == 0 })
		if len(m.prune.dialog.Selection()) != 1 {
			t.Fatalf("preview %+v", m.prune.dialog.Render(m.pruneArea()))
		}
	}
	execute := func() {
		pruneTestKey(t, m, uv.KeyEnter)
		pruneTestKey(t, m, uv.KeyEnter)
	}
	summary := func() string {
		box := m.prune.dialog.Render(m.pruneArea())
		return box.Detail + "\n" + strings.Join(box.Lines, "\n")
	}

	scan()
	candidate := m.prune.dialog.Selection()[0].Candidate
	paneID := 0
	for _, ws := range m.endpoints["local"].snapshot.State.Workspaces {
		if ws.ID == candidate.Workspace {
			paneID = ws.Tabs[0].Layout.Focused()
		}
	}
	if paneID == 0 {
		t.Fatal("candidate pane not found")
	}
	measure.arm(t)
	execute()
	measure.wait(t)
	draft := filepath.Join(path, "draft")
	if err := os.WriteFile(draft, []byte("unsaved during measurement"), 0600); err != nil {
		t.Fatal(err)
	}
	measure.release(t)
	h.until("dirty result", func() bool { return strings.Contains(summary(), "skipped 1") })
	if !strings.Contains(summary(), "dirty") {
		t.Fatal(summary())
	}
	h.api("local", api.MethodPaneGet, api.PaneTarget{PaneID: fmt.Sprintf("p%d", paneID)})
	if got, err := os.ReadFile(draft); err != nil || string(got) != "unsaved during measurement" {
		t.Fatalf("draft after rejected prune: %q, %v", got, err)
	}
	if err := os.Remove(draft); err != nil {
		t.Fatal(err)
	}
	pruneTestKey(t, m, uv.KeyEscape)

	scan()
	remove.arm(t)
	execute()
	remove.wait(t) // the target closed and the final use check already passed
	child := filepath.Join(path, "child")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	nested, branch := filepath.Join(alias, "new-worktree"), "new-worktree"
	for _, request := range []struct {
		method string
		params any
	}{
		{api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: &path}},
		{api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: &child}},
		{api.MethodTabCreate, api.TabCreateParams{Cwd: &child}},
		{api.MethodPaneSplit, api.PaneSplitParams{Cwd: &child, Direction: "right"}},
		{api.MethodWorktreeOpen, api.WorktreeOpenParams{Cwd: &path, Path: &path}},
		{api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &path, Path: &nested, Branch: &branch}},
	} {
		r := h.request("local", request.method, request.params)
		if r.Err == nil || !strings.Contains(r.Err.Message, "checkout is being pruned") {
			t.Fatalf("%s admitted use during removal: %+v", request.method, r)
		}
	}
	outside := t.TempDir()
	h.api("local", api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: &outside})
	status, err := exec.Command("git", "-C", path, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil || len(status) != 0 {
		t.Fatalf("checkout changed by admission tests: %q, %v", status, err)
	}
	remove.release(t)
	h.until("deletion result", func() bool { return len(m.prune.pending) == 0 })
	if !strings.Contains(summary(), "1 件削除") {
		output, _ := os.ReadFile(filepath.Join(gates, "git-local-review.output"))
		t.Fatalf("%s\nGit: %s", summary(), output)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkout still present: %v", err)
	}
}

func TestPruneCancelsBothScansAndRescansSameConnection(t *testing.T) {
	gates := t.TempDir()
	h := newRightMachineHarnessPeers(t, pruneGatedServerSetup(t, gates), []string{"local", "remote"}, true)
	local := newPruneToolGate(t, gates, "du-local-review")
	remote := newPruneToolGate(t, gates, "du-remote-review")
	m := h.m
	openPruneFixtureOnMachine(t, h, "local")
	openPruneFixtureOnMachine(t, h, "remote")
	local.arm(t)
	remote.arm(t)
	conns := [2]*endpointConnection{m.endpoints["local"].connection, m.endpoints["remote"].connection}
	m.openPruneWorkspaces()
	pruneTestKey(t, m, uv.KeyEnter)
	oldRequest := m.prune.request
	pids := []int{local.wait(t), remote.wait(t)}
	if len(m.prune.pending) != 2 {
		t.Fatal("expected two scans in flight")
	}
	pruneTestKey(t, m, uv.KeyEscape)
	local.disarm(t)
	remote.disarm(t)
	local.arm(t)
	m.openPruneWorkspaces()
	pruneTestKey(t, m, uv.KeyTab)
	pruneTestKey(t, m, uv.KeyEnter)
	local.wait(t)
	// A late cancellation for the old request must not retire the new scan.
	if err := conns[0].enqueue(msg{Type: endpoint.PruneScanCancel, PruneRequest: oldRequest}); err != nil {
		t.Fatal(err)
	}
	local.release(t)
	h.until("local-only rescan", func() bool { return len(m.prune.pending) == 0 })
	sel := m.prune.dialog.Selection()
	if len(sel) != 1 || sel[0].Machine != "local" || conns[0] != m.endpoints["local"].connection || conns[1] != m.endpoints["remote"].connection {
		t.Fatalf("rescan lost selection or connection: %+v", m.prune.dialog.Render(m.pruneArea()))
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for _, pid := range pids {
		for !errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
			select {
			case <-tick.C:
			case <-timer.C:
				t.Fatalf("cancelled scan subprocess %d was not reaped", pid)
			}
		}
	}
}

func TestPruneReservationRespectsFilesystemCase(t *testing.T) {
	cases := []struct{ name, mount string }{{name: "native_volume"}}
	if root := os.Getenv("IKESU_PRUNE_TEST_CASE_SENSITIVE_DIR"); root != "" {
		cases = append(cases, struct{ name, mount string }{"case_sensitive_volume", root})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gates := t.TempDir()
			h := newRightMachineHarnessPeers(t, pruneGatedServerSetup(t, gates), []string{"local"}, true)
			remove := newPruneToolGate(t, gates, "git-local-review")
			parent := t.TempDir()
			if tc.mount != "" {
				dir, err := os.MkdirTemp(tc.mount, "prune-case-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				parent = dir
			}
			path := openPruneFixtureIn(t, h, "local", parent)
			alternate := filepath.Join(parent, strings.ToUpper(filepath.Base(path)))
			original, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			other, err := os.Stat(alternate)
			if os.IsNotExist(err) {
				if err := os.MkdirAll(filepath.Join(alternate, "child"), 0700); err != nil {
					t.Fatal(err)
				}
				other, err = os.Stat(alternate)
			}
			if err != nil {
				t.Fatal(err)
			}
			same := os.SameFile(original, other)
			if tc.mount != "" && same {
				t.Fatal("the supplied case-sensitive volume resolved both spellings to the same inode")
			}
			t.Logf("case-variant paths refer to the same inode: %t", same)

			m := h.m
			m.openPruneWorkspaces()
			pruneTestKey(t, m, uv.KeyEnter)
			h.until("prune preview", func() bool { return len(m.prune.pending) == 0 })
			if len(m.prune.dialog.Selection()) != 1 {
				t.Fatal(m.prune.dialog.Render(m.pruneArea()))
			}
			remove.arm(t)
			pruneTestKey(t, m, uv.KeyEnter)
			pruneTestKey(t, m, uv.KeyEnter)
			remove.wait(t)

			for _, cwd := range []string{alternate, filepath.Join(alternate, "child")} {
				r := h.request("local", api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Cwd: &cwd})
				if same {
					if r.Err == nil || !strings.Contains(r.Err.Message, "checkout is being pruned") {
						t.Fatalf("same inode admitted at %q: %+v", cwd, r)
					}
				} else {
					var created api.WorkspaceCreatedResult
					if r.Err != nil || json.Unmarshal(r.Result, &created) != nil || created.RootPane.PaneID == "" {
						t.Fatalf("distinct inode denied at %q: %+v", cwd, r)
					}
					h.api("local", api.MethodPaneGet, api.PaneTarget{PaneID: created.RootPane.PaneID})
				}
			}
			if same {
				child := filepath.Join(alternate, "child", "not-yet-created")
				branch := "case-variant-child"
				r := h.request("local", api.MethodWorktreeCreate, api.WorktreeCreateParams{Cwd: &path, Path: &child, Branch: &branch})
				if r.Err == nil || !strings.Contains(r.Err.Message, "checkout is being pruned") {
					t.Fatalf("case-variant missing child admitted: %+v", r)
				}
				if _, err := os.Stat(child); !os.IsNotExist(err) {
					t.Fatalf("rejected child was created: %v", err)
				}
			}
			remove.release(t)
			h.until("deletion result", func() bool { return len(m.prune.pending) == 0 })
			if box := m.prune.dialog.Render(m.pruneArea()); !strings.Contains(box.Detail, "1 件削除") {
				t.Fatalf("prune result: %+v", box)
			}
			if !same {
				if _, err := os.Stat(alternate); err != nil {
					t.Fatalf("distinct directory did not survive: %v", err)
				}
			}
		})
	}
}

func TestPruneProtectsBackgroundJobStartedDuringMeasurement(t *testing.T) {
	gates := t.TempDir()
	setup := pruneGatedServerSetup(t, gates)
	dbPath := filepath.Join(gates, "jobs.db")
	h := newRightMachineHarnessPeers(t, func(cfg string) string {
		teststate.Setenv(t, dispatch.DBPathEnvVar, dbPath)
		return setup(cfg)
	}, []string{"local"}, true)
	measure := newPruneToolGate(t, gates, "du-local-review")
	jobGate := newPruneToolGate(t, gates, "job")
	path := openPruneFixtureOnMachine(t, h, "local")
	m := h.m
	scan := func() {
		m.openPruneWorkspaces()
		pruneTestKey(t, m, uv.KeyEnter)
		h.until("prune preview", func() bool { return len(m.prune.pending) == 0 })
	}
	scan()
	sel := m.prune.dialog.Selection()
	if len(sel) != 1 {
		t.Fatalf("expected one initial candidate: %+v", sel)
	}
	candidate := sel[0].Candidate
	state := m.endpoints["local"].snapshot.State
	caller := state.Workspaces[0]
	if caller.ID == candidate.Workspace {
		t.Fatal("background job must be launched from a different workspace")
	}
	var targetPane int
	for _, ws := range state.Workspaces {
		if ws.ID == candidate.Workspace {
			targetPane = ws.Tabs[0].Layout.Focused()
		}
	}
	if targetPane == 0 {
		t.Fatal("candidate pane not found")
	}
	measure.arm(t)
	pruneTestKey(t, m, uv.KeyEnter)
	pruneTestKey(t, m, uv.KeyEnter)
	measure.wait(t)

	child := filepath.Join(path, "child")
	r := h.request("local", api.MethodRunStart, api.RunStartParams{
		CallerPane: fmt.Sprintf("p%d", caller.Tabs[0].Layout.Focused()), Label: "prune-use-check",
		Cwd: child, Completion: "none",
		Argv: []string{"/bin/sh", "-c", `echo "$$" > "$1"; read -r go < "$2"`, "sh", jobGate.ready.Name(), jobGate.resume.Name()},
	})
	var started api.RunStartedResult
	if r.Err != nil || json.Unmarshal(r.Result, &started) != nil || started.Job == "" || started.Mode != "background" {
		t.Fatalf("background job did not start: %+v", r)
	}
	pid := jobGate.wait(t)
	got, gotErr := os.Stat(platform.ProcCwd(pid))
	want, wantErr := os.Stat(child)
	if gotErr != nil || wantErr != nil || !os.SameFile(got, want) {
		t.Fatalf("job did not run in target child directory: %v, %v", gotErr, wantErr)
	}
	measure.release(t)
	h.until("prune result while job running", func() bool { return len(m.prune.pending) == 0 })
	box := m.prune.dialog.Render(m.pruneArea())
	if text := strings.Join(box.Lines, "\n"); !strings.Contains(text, "skipped 1") || !strings.Contains(text, "checkout used by a background job") {
		t.Fatalf("prune failed to protect active job: %+v", box)
	}
	h.api("local", api.MethodPaneGet, api.PaneTarget{PaneID: fmt.Sprintf("p%d", targetPane)})
	if _, err := os.Stat(filepath.Join(child, "tracked")); err != nil {
		t.Fatalf("checkout removed while background job ran: %v", err)
	}

	pruneTestKey(t, m, uv.KeyEscape)
	scan()
	if box := m.prune.dialog.Render(m.pruneArea()); len(m.prune.dialog.Selection()) != 0 || !strings.Contains(strings.Join(box.Lines, "\n"), "checkout used by a background job") {
		t.Fatalf("scan failed to protect active job: %+v", box)
	}
	if _, err := jobGate.resume.WriteString("done\n"); err != nil {
		t.Fatal(err)
	}
	store, err := dispatch.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		row, err := store.CommandRow(started.Job)
		if err != nil || row == nil {
			t.Fatalf("background dispatch row: %+v, %v", row, err)
		}
		if row.Status == "exited" {
			if row.ExitCode == nil || *row.ExitCode != 0 {
				t.Fatalf("background job exit: %+v", row)
			}
			break
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatalf("dispatch did not record job completion: %+v", row)
		}
	}
	pruneTestKey(t, m, uv.KeyEscape)
	scan()
	if sel := m.prune.dialog.Selection(); len(sel) != 1 || sel[0].Candidate.Workspace != candidate.Workspace {
		t.Fatalf("completed job still protected the checkout: %+v", m.prune.dialog.Render(m.pruneArea()))
	}
}
