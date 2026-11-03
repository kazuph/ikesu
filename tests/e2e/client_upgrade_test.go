package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/tui"
)

func buildClientUpgradeVersions(t *testing.T) (dir, oldBinary, newBinary, oldVersion, newVersion string) {
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
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {mainPath: newSource}})
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
	s.waitFor("new client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+newVersion) })
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
	s.waitFor("upgrade rejection", 15*time.Second, func(text string) bool { return strings.Contains(text, "client upgrade:") })
	if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) {
		t.Fatal("client left its terminal after executable verification failed")
	}
	s.send("printf 'REJECTED_UPGRADE_INPUT_OK\\n'\r")
	s.waitFor("input after rejected upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "REJECTED_UPGRADE_INPUT_OK") })
}
