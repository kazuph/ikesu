package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/client"
	"github.com/kazuph/ikesu/internal/tui"
)

func buildClientUpgradeVersions(t *testing.T) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	return buildClientUpgradeVersionsWithShortVersionTimeout(t, false)
}

// Real client builds and the stalled welcome rollback can exceed five seconds under load.
func TestClientRollsBackWhenWelcomeStalls(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersionsWithShortAttachTimeout(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old server", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	socket := filepath.Join(s.stateDir, s.name, "ikesu-client.sock")
	held := socket + ".held"
	if err := os.Rename(socket, held); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(held, socket) }()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	state, err := json.Marshal(map[string]any{
		"old_executable": oldBinary, "old_version": oldVersion, "session": "e2e", "active": "local",
		"attempt": map[string]any{"session": "e2e", "generation": "stalled-welcome", "executable": newBinary, "version": newVersion},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := startVersionMode(t, map[string]string{"IKESU_STATE_DIR": s.stateDir, "IKESU_CLIENT_UPGRADE": string(state)}, false, newBinary, "")
	if err := listener.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	accepted, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	client.send("printf 'BUFFERED_DURING_STALLED_WELCOME\\n'\r")
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(held, socket); err != nil {
		t.Fatal(err)
	}
	client.waitFor("welcome timeout rollback", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: read unix") })
	client.waitFor("input during stalled welcome", 10*time.Second, func(text string) bool { return strings.Contains(text, "BUFFERED_DURING_STALLED_WELCOME") })
	resolvedOld, err := filepath.EvalSymlinks(oldBinary)
	if err != nil {
		t.Fatal(err)
	}
	process, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(client.cmd.Process.Pid), "-d", "txt", "-Fn").Output()
	if err != nil || (!strings.Contains(string(process), "n"+oldBinary+"\n") && !strings.Contains(string(process), "n"+resolvedOld+"\n")) {
		t.Fatalf("old executable was not restored: %q %v", process, err)
	}
	client.send("printf 'ROLLBACK_AFTER_STALLED_WELCOME\\n'\r")
	client.waitFor("input after stalled welcome", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_AFTER_STALLED_WELCOME") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if !strings.Contains(read.Read.Text, "ROLLBACK_AFTER_STALLED_WELCOME") || !strings.Contains(read.Read.Text, "BUFFERED_DURING_STALLED_WELCOME") {
		t.Fatalf("input did not reach the original pane: %s", read.Read.Text)
	}
}

func buildClientUpgradeVersionsWithShortVersionTimeout(t *testing.T, shortTimeout bool) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	return buildClientUpgradeVersionsWithTimeouts(t, shortTimeout, false, false)
}

func buildClientUpgradeVersionsWithShortAttachTimeout(t *testing.T) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	return buildClientUpgradeVersionsWithTimeouts(t, false, true, false)
}

func buildStoppedClientUpgradeVersions(t *testing.T, shortAttach bool) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	return buildClientUpgradeVersionsWithTimeouts(t, false, shortAttach, true)
}

type upgradeVersionBuild struct {
	once                   sync.Once
	old, new               []byte
	oldVersion, newVersion string
	err                    error
}

var upgradeVersionBuilds sync.Map

func buildClientUpgradeVersionsWithTimeouts(t *testing.T, shortTimeout, shortAttach, stopBeforePing bool) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	t.Helper()
	key := [3]bool{shortTimeout, shortAttach, stopBeforePing}
	value, _ := upgradeVersionBuilds.LoadOrStore(key, &upgradeVersionBuild{})
	build := value.(*upgradeVersionBuild)
	build.once.Do(func() {
		build.err = fmt.Errorf("test version build did not finish")
		_, oldPath, newPath, oldVersion, newVersion := buildClientUpgradeVersionsUncached(t, shortTimeout, shortAttach, stopBeforePing)
		oldData, err := os.ReadFile(oldPath)
		if err != nil {
			build.err = err
			return
		}
		newData, err := os.ReadFile(newPath)
		if err != nil {
			build.err = err
			return
		}
		build.old, build.new = oldData, newData
		build.oldVersion, build.newVersion = oldVersion, newVersion
		build.err = nil
	})
	if build.err != nil {
		t.Fatal(build.err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err = os.MkdirTemp(home, ".ikesu-client-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	oldBinary, newBinary = filepath.Join(dir, "old-ikesu"), filepath.Join(dir, "new-ikesu")
	if err := os.WriteFile(oldBinary, build.old, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBinary, build.new, 0700); err != nil {
		t.Fatal(err)
	}
	return dir, oldBinary, newBinary, build.oldVersion, build.newVersion
}

func buildClientUpgradeVersionsUncached(t *testing.T, shortTimeout, shortAttach, stopBeforePing bool) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err = os.MkdirTemp(home, ".ikesu-client-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	oldBinary = filepath.Join(dir, "old-ikesu")
	if err := os.WriteFile(oldBinary, data, 0700); err != nil {
		t.Fatal(err)
	}
	versionOutput, err := exec.Command(oldBinary, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	oldVersion = strings.TrimPrefix(strings.TrimSpace(string(versionOutput)), "ikesu ")
	newVersion = oldVersion + "x"
	mainPath := filepath.Join(repoRoot(), "cmd/ikesu/main.go")
	source, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	oldDecl := "const version = \"" + oldVersion + "\""
	if !strings.Contains(string(source), oldDecl) {
		t.Fatalf("version declaration %q missing", oldDecl)
	}
	newSource := filepath.Join(dir, "main.go")
	if err := os.WriteFile(newSource, []byte(strings.Replace(string(source), oldDecl, "var version = \""+oldVersion+"\"", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	replacements := map[string]string{mainPath: newSource}
	oldReplacements := map[string]string{}
	if shortAttach {
		for _, replacement := range []struct{ file, original, shortened string }{
			{"client.go", "waitDial(dir, attachTimeout)", "waitDial(dir, 250 * time.Millisecond)"},
			{"machine_connections.go", "SetDeadline(time.Now().Add(attachTimeout))", "SetDeadline(time.Now().Add(250 * time.Millisecond))"},
			{"retry.go", "connectionNotice  = 10 * time.Second", "connectionNotice  = time.Second"},
		} {
			path := filepath.Join(repoRoot(), "internal/client", replacement.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), replacement.original) {
				t.Fatalf("missing timeout declaration in %s", path)
			}
			shortSource := filepath.Join(dir, replacement.file)
			if err := os.WriteFile(shortSource, []byte(strings.Replace(string(data), replacement.original, replacement.shortened, 1)), 0600); err != nil {
				t.Fatal(err)
			}
			replacements[path] = shortSource
			oldReplacements[path] = shortSource
		}
	}
	if shortTimeout {
		upgradePath := filepath.Join(repoRoot(), "internal/client/upgrade_executable.go")
		upgradeSource, err := os.ReadFile(upgradePath)
		if err != nil {
			t.Fatal(err)
		}
		original := "context.WithTimeout(ctx, attachTimeout)"
		index := strings.LastIndex(string(upgradeSource), original)
		if index < 0 {
			t.Fatal("version timeout call missing")
		}
		shortSource := filepath.Join(dir, "upgrade_executable.go")
		text := string(upgradeSource[:index]) + "context.WithTimeout(ctx, 250*time.Millisecond)" + string(upgradeSource[index+len(original):])
		text = strings.Replace(text, "import (", "import (\n\"time\"", 1)
		if err := os.WriteFile(shortSource, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		oldReplacements[upgradePath] = shortSource
		replacements[upgradePath] = shortSource
	}
	if stopBeforePing {
		clientPath := filepath.Join(repoRoot(), "internal/client/client.go")
		sourcePath := clientPath
		if path, ok := oldReplacements[clientPath]; ok {
			sourcePath = path
		}
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		boundary := "\t\t\tcurrent, err = queryUpgradeIdentity(context.Background(), session)"
		if strings.Count(string(source), boundary) != 1 {
			t.Fatal("upgrade identity boundary changed")
		}
		instrumented := filepath.Join(dir, "client-stop.go")
		text := strings.Replace(string(source), boundary, "\t\t\t_ = syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)\n"+boundary, 1)
		if err := os.WriteFile(instrumented, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		oldReplacements[clientPath] = instrumented
	}
	var concurrentOldBuild *exec.Cmd
	if len(oldReplacements) != 0 {
		oldOverlay, err := json.Marshal(map[string]map[string]string{"Replace": oldReplacements})
		if err != nil {
			t.Fatal(err)
		}
		oldOverlayPath := filepath.Join(dir, "old-overlay.json")
		if err := os.WriteFile(oldOverlayPath, oldOverlay, 0600); err != nil {
			t.Fatal(err)
		}
		build := exec.Command("go", "build", "-overlay", oldOverlayPath, "-o", oldBinary, "./cmd/ikesu")
		build.Dir = repoRoot()
		if shortTimeout && !shortAttach && !stopBeforePing {
			concurrentOldBuild = build
		} else if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build old test client: %v %s", err, out)
		}
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": replacements})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	newBinary = filepath.Join(dir, "new-ikesu")
	build := exec.Command("go", "build", "-overlay", overlayPath, "-ldflags", "-X main.version="+newVersion, "-o", newBinary, "./cmd/ikesu")
	build.Dir = repoRoot()
	var oldOutput bytes.Buffer
	if concurrentOldBuild != nil {
		concurrentOldBuild.Stdout = &oldOutput
		concurrentOldBuild.Stderr = &oldOutput
		if err := concurrentOldBuild.Start(); err != nil {
			t.Fatalf("start old test client build: %v", err)
		}
	}
	out, buildErr := build.CombinedOutput()
	if concurrentOldBuild != nil {
		if err := concurrentOldBuild.Wait(); err != nil {
			t.Fatalf("build old test client: %v %s", err, oldOutput.String())
		}
	}
	if buildErr != nil {
		t.Fatalf("build new version: %v %s", buildErr, out)
	}
	return
}

// Two real clients and two builds are needed to test per-terminal selection across exec.
func TestClientUpgradePreservesTwoSelections(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	_, oldBinary, newBinary, _, newVersion := buildClientUpgradeVersions(t)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[keys]\nswitch_workspace='cmd+1..9'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := startVersionMode(t, map[string]string{"IKESU_CONFIG": cfg}, false, oldBinary, oldBinary)
	first.waitFor("first shell", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	second := startVersionMode(t, map[string]string{"IKESU_STATE_DIR": first.stateDir, "IKESU_CONFIG": cfg}, false, oldBinary, "")
	second.waitFor("second shell", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	second.send("\x02c\r")
	second.waitFor("second tab", 10*time.Second, func(text string) bool { return strings.Contains(text, "%2 terminal") && strings.Contains(text, "❯") })
	var space api.WorkspaceCreatedResult
	first.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, &space)
	first.send("\x1b[50;9u")
	first.waitFor("first workspace", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") && !strings.Contains(text, "%2 terminal") })
	proof := func(s *session, pane, label string) {
		t.Helper()
		marker := "CLIENT_UPGRADE_" + label
		s.send(fmt.Sprintf("printf 'CLIENT_UPGRADE_%%s\\n' '%s'\r", label))
		s.waitFor(label, 10*time.Second, func(text string) bool { return strings.Contains(text, marker) })
		var read api.PaneReadResultEnvelope
		first.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: pane, StripANSI: true, Source: "recent", Format: "text"}, &read)
		if !strings.Contains(read.Read.Text, marker) {
			t.Fatalf("%s input did not reach %s: %s", label, pane, read.Read.Text)
		}
	}
	proof(first, space.RootPane.PaneID, "FIRST_BEFORE")
	proof(second, "p2", "SECOND_BEFORE")
	if out, err := first.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	for _, s := range []*session{first, second} {
		s.waitFor("new client", 15*time.Second, func(text string) bool {
			if strings.Contains(text, "upgrade:") {
				t.Fatalf("client upgrade failed:\n%s", text)
			}
			return strings.Contains(text, "ikesu v"+newVersion)
		})
	}
	proof(first, space.RootPane.PaneID, "FIRST_AFTER")
	proof(second, "p2", "SECOND_AFTER")
}

// Many panes and their output make the real handoff carry a substantial state.
func TestClientUpgradeWithLoadedServer(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	const paneCount = 12
	for i := 0; i < paneCount; i++ {
		var workspace api.WorkspaceCreatedResult
		s.rpc(api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: false}, &workspace)
		pane := workspace.RootPane.PaneID
		s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: pane, Text: "yes 'handoff-load-output-0123456789' | head -n 2000; printf 'LOAD_%s\\n' COMPLETE", Keys: []string{"enter"}}, nil)
		ms := uint64(15000)
		s.rpc(api.MethodPaneWaitForOutput, api.PaneWaitForOutputParams{PaneID: pane, Source: "recent", Match: api.OutputMatch{Type: "substring", Value: "LOAD_COMPLETE"}, TimeoutMs: &ms}, nil)
	}
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	if len(panes.Panes) < paneCount+1 {
		t.Fatalf("prepared only %d panes", len(panes.Panes))
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	s.waitFor("new client", 30*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+newVersion) })
	process, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(s.cmd.Process.Pid), "-d", "txt", "-Fn").Output()
	if err != nil || !strings.Contains(string(process), "n"+newBinary+"\n") {
		t.Fatalf("new executable not mapped: %q %v", process, err)
	}
	t.Logf("%d panes generated 2000 lines each; client pid=%d lsof=%s", len(panes.Panes), s.cmd.Process.Pid, strings.TrimSpace(string(process)))
}

// The second build exercises actual client exec across two server versions.
func TestClientFollowsLiveHandoffAndRollback(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	var output bytes.Buffer
	s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
	if out, err := s.cli("server", "live-handoff", "--import-exe", oldBinary); err != nil {
		t.Fatalf("same-version handoff: %v %s", err, out)
	}
	s.send("printf 'SAME_VERSION_INPUT_OK\\n'\r")
	s.waitFor("input after same-version handoff", 10*time.Second, func(text string) bool { return strings.Contains(text, "SAME_VERSION_INPUT_OK") })
	if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) || bytes.Contains(output.Bytes(), []byte(tui.EnterSeq)) {
		t.Fatal("same-version handoff restarted the client")
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("upgrade server: %v %s", err, out)
	}
	s.waitFor("new client", 15*time.Second, func(text string) bool {
		if strings.Contains(text, "upgrade:") {
			t.Fatalf("client upgrade failed:\n%s", text)
		}
		return strings.Contains(text, "ikesu v"+newVersion)
	})
	s.send("printf 'UPGRADE_CLIENT_INPUT_OK\\n'\r")
	s.waitFor("input after upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "UPGRADE_CLIENT_INPUT_OK") })
	if !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) || !bytes.Contains(output.Bytes(), []byte(tui.KbdLeaveSeq()+tui.LeaveSeq)) || !bytes.Contains(output.Bytes(), []byte(tui.EnterSeq+tui.KbdEnterSeq())) {
		t.Fatalf("missing terminal leave/re-entry sequences: %q", output.Bytes())
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", oldBinary); err != nil {
		t.Fatalf("rollback server: %v %s", err, out)
	}
	s.waitFor("rolled back client", 15*time.Second, func(text string) bool {
		return strings.Contains(text, "ikesu v"+oldVersion) && !strings.Contains(text, newVersion)
	})
	s.send("printf 'ROLLBACK_CLIENT_INPUT_OK\\n'\r")
	s.waitFor("input after rollback", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_CLIENT_INPUT_OK") })
	for _, seq := range []struct{ name, value string }{
		{"mouse off", tui.MouseOffSeq},
		{"keyboard and alternate screen leave", tui.KbdLeaveSeq() + tui.LeaveSeq},
		{"alternate screen and keyboard enter", tui.EnterSeq + tui.KbdEnterSeq()},
	} {
		if count := bytes.Count(output.Bytes(), []byte(seq.value)); count != 2 {
			t.Fatalf("%s: got %d sequences for two transitions", seq.name, count)
		}
	}
}

// A client started through the entry symlink must follow the server even
// when the upgrade script has already repointed that symlink to the new
// release. The symlink is moved before the handoff so the client always
// checks its own executable after the move.
func TestClientFollowsWhenEntrySymlinkMovedFirst(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
	entry := filepath.Join(t.TempDir(), "ikesu")
	if err := os.Symlink(oldBinary, entry); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, false, entry, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	next := entry + ".next"
	if err := os.Symlink(newBinary, next); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, entry); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("upgrade server: %v %s", err, out)
	}
	s.waitFor("new client", 15*time.Second, func(text string) bool {
		if strings.Contains(text, "upgrade:") {
			t.Fatalf("client upgrade failed:\n%s", text)
		}
		return strings.Contains(text, "ikesu v"+newVersion)
	})
	s.send("printf 'SYMLINK_%s\\n' 'UPGRADE_INPUT_OK'\r")
	s.waitFor("input after upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "SYMLINK_UPGRADE_INPUT_OK") })
}

// Building the second version and completing the live handoff takes over five seconds.
func TestClientKeepsRunningWhenUpgradeDirectoryWritable(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	dir, oldBinary, newBinary, oldVersion, _ := buildClientUpgradeVersions(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	var output bytes.Buffer
	s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff to server in writable directory: %v %s", err, out)
	}
	s.waitFor("upgrade rejection", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade:") })
	if text := s.text(); !strings.Contains(text, "writable") && !strings.Contains(text, "untrusted") {
		t.Fatalf("permission rejection did not show its reason:\n%s", text)
	}
	if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) {
		t.Fatal("client left its terminal after executable verification failed")
	}
	s.send("printf 'REJECTED_UPGRADE_INPUT_OK\\n'\r")
	s.waitFor("input after rejected upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "REJECTED_UPGRADE_INPUT_OK") })
}

// Building two real client versions and running an isolated server and client
// measured over five seconds when the tests ran concurrently.
func TestClientRollsBackWhenNewConnectionFails(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersionsWithShortAttachTimeout(t)
	state := map[string]any{
		"old_executable": oldBinary,
		"old_version":    oldVersion,
		"session":        "e2e",
		"attempt": map[string]any{
			"session": "e2e", "generation": "connection-failure-test", "executable": newBinary, "version": newVersion,
		},
		"active": "local",
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	// No server is started: the new client must exec the old binary, which
	// starts its own isolated session using the normal attach path.
	s := startVersionMode(t, map[string]string{"IKESU_CLIENT_UPGRADE": string(raw)}, false, newBinary, "")
	s.waitFor("connection failure notice", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: connect") })
	s.send("printf 'ROLLBACK_AFTER_CONNECT_FAIL\\n'\r")
	s.waitFor("input after connection rollback", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_AFTER_CONNECT_FAIL") })
	s.waitFor("old client version", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
}

// Real client builds and a configuration rollback can exceed five seconds under load.
func TestClientRollsBackWhenNewConfigFails(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersionsWithShortAttachTimeout(t)
	state := map[string]any{
		"old_executable": oldBinary,
		"old_version":    oldVersion,
		"session":        "e2e",
		"attempt": map[string]any{
			"session": "e2e", "generation": "configuration-failure-test", "executable": newBinary, "version": newVersion,
		},
		"active": "local",
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(cfg, []byte("invalid = [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prepare := func(cmd *exec.Cmd) {
		cmd.Env = append(cmd.Env, "IKESU_CLIENT_UPGRADE="+string(raw))
	}
	s := startVersionModePrepared(t, map[string]string{"IKESU_CONFIG": cfg}, false, newBinary, oldBinary, prepare)
	s.waitFor("configuration failure notice", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: client config") })
	// Invalid configuration falls back to onboarding defaults; dismiss the real
	// dialog before sending shell input so it cannot consume the command.
	s.waitFor("onboarding after configuration rollback", 15*time.Second, func(text string) bool { return strings.Contains(text, "terminal workspace manager for coding agents") })
	s.send("\r")
	s.waitFor("onboarding dismissed", 15*time.Second, func(text string) bool { return !strings.Contains(text, "terminal workspace manager for coding agents") })
	s.waitFor("old shell after configuration rollback", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.waitFor("old client version after configuration rollback", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	s.send("printf 'ROLLBACK_AFTER_CONFIG_FAIL\\n'\r")
	s.waitFor("input after configuration rollback", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_AFTER_CONFIG_FAIL") })
	var pong api.PongResult
	s.rpc(api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Version != oldVersion {
		t.Fatalf("old client did not start the old server: %+v", pong)
	}
}

func TestClientRawModeFailureHelper(t *testing.T) {
	if os.Getenv("CLIENT_UPGRADE_RAW_HELPER") != "1" {
		return
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	old := os.Getenv("CLIENT_UPGRADE_RAW_OLD")
	version := os.Getenv("CLIENT_UPGRADE_RAW_VERSION")
	os.Args = []string{old, "--session", "e2e"}
	if _, err := client.Attach("e2e", r, os.Stdout, version); err != nil {
		t.Fatalf("raw failure did not exec the old client: %v", err)
	}
	t.Fatal("raw failure returned without restoring the old client")
}

// A real pipe makes MakeRaw fail while stdin remains a usable PTY for rollback.
func TestClientRollsBackWhenRawModeFails(t *testing.T) {
	if runIsolatedParallel(t) {
		return
	}
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
	state, err := json.Marshal(map[string]any{
		"old_executable": oldBinary, "old_version": oldVersion, "session": "e2e", "active": "local",
		"attempt": map[string]any{"session": "e2e", "generation": "raw-failure-test", "executable": newBinary, "version": newVersion},
	})
	if err != nil {
		t.Fatal(err)
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(cmd *exec.Cmd) {
		cmd.Args = []string{helper, "-test.run=^TestClientRawModeFailureHelper$"}
		cmd.Env = append(cmd.Env, "CLIENT_UPGRADE_RAW_HELPER=1", "CLIENT_UPGRADE_RAW_OLD="+oldBinary, "CLIENT_UPGRADE_RAW_VERSION="+newVersion, "IKESU_CLIENT_UPGRADE="+string(state))
	}
	s := startVersionModePrepared(t, nil, false, helper, oldBinary, prepare)
	s.waitFor("raw failure notice", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: raw mode") })
	s.waitFor("old shell after raw failure", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	s.send("printf 'ROLLBACK_AFTER_RAW_FAIL\\n'\r")
	s.waitFor("input after raw failure", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_AFTER_RAW_FAIL") })
	var pong api.PongResult
	s.rpc(api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Version != oldVersion {
		t.Fatalf("unexpected server after raw rollback: %+v", pong)
	}
}
