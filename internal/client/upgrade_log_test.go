package client

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestUpgradeFailureLog(t *testing.T) {
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	dir := filepath.Join(root, "isolated")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := upgradeIdentity{Version: "0.4.75", Executable: "/release/new/ikesu"}
	logUpgradeFailure("isolated", "0.4.74", id, errors.New("version mismatch"))
	path := filepath.Join(dir, "client-upgrade.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Time           string `json:"time"`
		CurrentVersion string `json:"current_version"`
		TargetVersion  string `json:"target_version"`
		Executable     string `json:"executable"`
		Reason         string `json:"reason"`
	}
	if err := json.Unmarshal(data, &row); err != nil {
		t.Fatal(err)
	}
	if row.Time == "" || row.CurrentVersion != "0.4.74" || row.TargetVersion != "0.4.75" || row.Executable != id.Executable || row.Reason != "version mismatch" {
		t.Fatalf("unexpected record: %+v", row)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("log mode: %v %v", info, err)
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

func TestUpgradeFailureReasonDoesNotCopyUnknownText(t *testing.T) {
	for _, err := range []error{errors.New("untrusted SECRET_INPUT"), &api.Error{Message: "SECRET_INPUT"}} {
		if got := upgradeFailureReason(err); strings.Contains(got, "SECRET_INPUT") {
			t.Fatalf("untrusted content leaked: %q", got)
		}
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
