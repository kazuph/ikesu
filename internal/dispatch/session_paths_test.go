package dispatch

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"path/filepath"
	"testing"
)

func TestSessionDatabasePathsPreserveDefaultAndExplicitOverride(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", root)
	teststate.Setenv(t, "IKESU_DB_PATH", "")
	if got := DBPath(); got != filepath.Join(root, "ikesu.db") {
		t.Fatal(got)
	}
	if DBPath("default") != DBPath() || DBPath("") != DBPath() {
		t.Fatal("default database moved")
	}
	legacy, err := Open(DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.InsertMessage("legacy", "", "before", "owner", "kept", nil, false); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	for _, name := range []string{"aa", "bb"} {
		want := filepath.Join(root, "sessions", name, "ikesu.db")
		if got := DBPath(name); got != want {
			t.Fatalf("%s: %s", name, got)
		}
		if got := JobLogPath("j0001", "label", name); got != filepath.Join(root, "sessions", name, "job-logs", "j0001-label.log") {
			t.Fatal(got)
		}
		store, err := Open(DBPath(name))
		if err != nil {
			t.Fatal(err)
		}
		rooms, err := store.Rooms()
		store.Close()
		if err != nil || len(rooms) != 0 {
			t.Fatalf("named session read legacy data: %v %v", rooms, err)
		}
	}
	legacy, err = OpenReadOnly(DBPath("default"))
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	rows, err := legacy.History("legacy", nil, 10)
	if err != nil || len(rows) != 1 || rows[0].Body != "kept" {
		t.Fatalf("legacy data lost: %v %v", rows, err)
	}
	explicit := filepath.Join(root, "chosen.db")
	teststate.Setenv(t, "IKESU_DB_PATH", explicit)
	for _, name := range []string{"default", "aa", "bb"} {
		if DBPath(name) != explicit {
			t.Fatal("explicit database override ignored", name)
		}
	}
}
