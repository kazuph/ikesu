package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
)

func TestUpgradeWaitLogReasonsAreFixed(t *testing.T) {
	for _, reason := range []upgradeWait{
		"cannot send pending input before client upgrade",
		"input destination is changing during client upgrade",
		"local input is waiting for reconnection before client upgrade",
		"clipboard input is pending before client upgrade",
	} {
		if got := upgradeFailureReason(reason); got != "waiting to retry client upgrade: "+string(reason) {
			t.Fatalf("wait reason %q: %q", reason, got)
		}
	}
	if got := upgradeFailureReason(upgradeWait("untrusted secret")); got != "client upgrade failed; details omitted" {
		t.Fatalf("unexpected wait reason: %q", got)
	}
}

func TestUpgradeFailureLogStopsAtLimit(t *testing.T) {
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	dir := filepath.Join(root, "isolated")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client-upgrade.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(upgradeLogLimit - int64(len(upgradeLogLimitMessage))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	logUpgradeFailure("isolated", "old", upgradeIdentity{Version: "new"}, errors.New("failure"))
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(first)) > upgradeLogLimit || !strings.HasSuffix(string(first), upgradeLogLimitMessage) {
		t.Fatal("missing limit marker or log exceeds limit")
	}
	logUpgradeFailure("isolated", "old", upgradeIdentity{Version: "new"}, errors.New("failure"))
	info, err := os.Stat(path)
	if err != nil || info.Size() != int64(len(first)) {
		t.Fatalf("log grew after limit: %v %v", info, err)
	}
}

func TestUpgradeFailureLogOmitsUntrustedErrorContents(t *testing.T) {
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	dir := filepath.Join(root, "isolated")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	logUpgradeFailure("isolated", "old", upgradeIdentity{Version: "new"}, errors.New("client configuration: invalid color: SECRET_VALUE"))
	data, err := os.ReadFile(filepath.Join(dir, "client-upgrade.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SECRET_VALUE") {
		t.Fatalf("configuration value leaked: %s", data)
	}
	if !strings.Contains(string(data), "client configuration") {
		t.Fatalf("failure type absent: %s", data)
	}
}

func TestUpgradeFailureLogMetadataIsBounded(t *testing.T) {
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	dir := filepath.Join(root, "isolated")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ current, target, executable, wantCurrent, wantTarget, wantPath string }{
		{"0.4.74", "0.4.75", "/release/ikesu", "0.4.74", "0.4.75", "/release/ikesu"},
		{"invalid\n0.4.74", "1.2.3\x1b[31m", "/tmp/a\n\x1b\u0085\u202eb", "invalid", "invalid", "/tmp/ab"},
		{"0.4.74", strings.Repeat("9", 513) + ".2.3", strings.Repeat("あ", 200), "0.4.74", "invalid", strings.Repeat("あ", 170)},
	}
	for _, c := range cases {
		logUpgradeFailure("isolated", c.current, upgradeIdentity{Version: c.target, Executable: c.executable}, errors.New("version mismatch"))
	}
	data, err := os.ReadFile(filepath.Join(dir, "client-upgrade.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("lines=%d", len(lines))
	}
	for i, line := range lines {
		var row struct {
			Current string `json:"current_version"`
			Target  string `json:"target_version"`
			Path    string `json:"executable"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		c := cases[i]
		if row.Current != c.wantCurrent || row.Target != c.wantTarget || row.Path != c.wantPath {
			t.Errorf("case %d: %+v", i, row)
		}
		if len(row.Path) > 512 || len(row.Current) > 512 || len(row.Target) > 512 {
			t.Errorf("case %d exceeds limit", i)
		}
	}
}
