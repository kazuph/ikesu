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

func TestUpgradeFailureLog(t *testing.T) {
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	dir := filepath.Join(root, "isolated")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := upgradeIdentity{Version: "new", Executable: "/release/new/ikesu"}
	logUpgradeFailure("isolated", "old", id, errors.New("version mismatch"))
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
	if row.Time == "" || row.CurrentVersion != "old" || row.TargetVersion != "new" || row.Executable != id.Executable || row.Reason != "version mismatch" {
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
