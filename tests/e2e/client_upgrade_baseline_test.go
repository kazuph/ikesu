//go:build client_upgrade_baseline

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/tui"
)

// This test requires a separately built real server from before client follow-upgrade.
func TestClientReconnectsToBaselineServerWithoutIdentity(t *testing.T) {
	baseline := os.Getenv("CLIENT_UPGRADE_BASELINE_SERVER")
	if baseline == "" || !filepath.IsAbs(baseline) {
		t.Fatal("CLIENT_UPGRADE_BASELINE_SERVER must name the absolute baseline executable")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".ikesu-baseline-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	client := filepath.Join(dir, "ikesu")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client, data, 0700); err != nil {
		t.Fatal(err)
	}
	clientOutput, err := exec.Command(client, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	clientVersion := strings.TrimPrefix(strings.TrimSpace(string(clientOutput)), "ikesu ")
	s := startVersionMode(t, nil, false, client, baseline)
	s.waitFor("new client on baseline server", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+clientVersion) })
	var pong api.PongResult
	s.rpc(api.MethodPing, api.EmptyParams{}, &pong)
	if pong.Version == clientVersion || pong.Executable != "" || pong.Generation != "" {
		t.Fatalf("baseline must have a different version and no identity: %+v", pong)
	}
	var output bytes.Buffer
	s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
	if out, err := s.cli("server", "live-handoff", "--import-exe", baseline); err != nil {
		t.Fatalf("baseline handoff: %v %s", err, out)
	}
	s.send("printf 'BASELINE_RECONNECT_INPUT_OK\\n'\r")
	s.waitFor("input after baseline handoff", 15*time.Second, func(text string) bool { return strings.Contains(text, "BASELINE_RECONNECT_INPUT_OK") })
	if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) || !strings.Contains(s.text(), "ikesu v"+clientVersion) {
		t.Fatal("client restarted or changed version on a baseline server")
	}
}
