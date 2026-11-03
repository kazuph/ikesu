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
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/client"
	"github.com/kazuph/ikesu/internal/tui"
)

func buildClientUpgradeVersions(t *testing.T) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
	return buildClientUpgradeVersionsWithShortVersionTimeout(t, false)
}

// The real ten-second attach deadline is needed to exercise the complete rollback path.
func TestClientRollsBackWhenWelcomeStalls(t *testing.T) {
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
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
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(held, socket); err != nil {
		t.Fatal(err)
	}
	client.waitFor("welcome timeout rollback", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: read unix") })
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
	if !strings.Contains(read.Read.Text, "ROLLBACK_AFTER_STALLED_WELCOME") {
		t.Fatalf("input did not reach the original pane: %s", read.Read.Text)
	}
}

func buildClientUpgradeVersionsWithShortVersionTimeout(t *testing.T, shortTimeout bool) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
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
	if err := os.WriteFile(newSource, []byte(strings.Replace(string(source), oldDecl, "const version = \""+newVersion+"\"", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	replacements := map[string]string{mainPath: newSource}
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
		oldOverlay, err := json.Marshal(map[string]map[string]string{"Replace": {upgradePath: shortSource}})
		if err != nil {
			t.Fatal(err)
		}
		oldOverlayPath := filepath.Join(dir, "old-overlay.json")
		if err := os.WriteFile(oldOverlayPath, oldOverlay, 0600); err != nil {
			t.Fatal(err)
		}
		build := exec.Command("go", "build", "-overlay", oldOverlayPath, "-o", oldBinary, "./cmd/ikesu")
		build.Dir = repoRoot()
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build old test client: %v %s", err, out)
		}
		replacements[upgradePath] = shortSource
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
	build := exec.Command("go", "build", "-overlay", overlayPath, "-o", newBinary, "./cmd/ikesu")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build new version: %v %s", err, out)
	}
	return
}

// Two real clients and two builds are needed to test per-terminal selection across exec.
func TestClientUpgradePreservesTwoSelections(t *testing.T) {
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

// The second build exercises actual client exec across two server versions.
func TestClientFollowsLiveHandoffAndRollback(t *testing.T) {
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

// Building the second version and completing the live handoff takes over five seconds.
func TestClientKeepsRunningWhenUpgradeDirectoryWritable(t *testing.T) {
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

// The existing notice duration is ten seconds, so checking the restored version takes longer than five seconds.
func TestClientRollsBackWhenNewConnectionFails(t *testing.T) {
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
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

// The ten-second notice must clear before testing normal input after rollback.
func TestClientRollsBackWhenNewConfigFails(t *testing.T) {
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
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
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	prepare := func(cmd *exec.Cmd) {
		if err := os.WriteFile(cfg, []byte("invalid = [\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cmd.Env = append(cmd.Env, "IKESU_CLIENT_UPGRADE="+string(raw))
	}
	s := startVersionModePrepared(t, map[string]string{"IKESU_CONFIG": cfg}, false, newBinary, oldBinary, prepare)
	s.waitFor("configuration failure notice", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: client config") })
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
