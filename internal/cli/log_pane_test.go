package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestLogFindsPaneJobWithoutDatabase(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	teststate.Setenv(t, "IKESU_STATE_DIR", dir)
	teststate.Setenv(t, "IKESU_DB_PATH", filepath.Join(dir, "absent.db"))
	if err := os.MkdirAll(dispatch.JobLogDir(), 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"j1", "legacy-id"} {
		if err := os.WriteFile(filepath.Join(dispatch.JobLogDir(), id+"-label.log"), []byte("contents "+id+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		if code := Run([]string{"log", id}, Options{Stdout: &out, Stderr: &errs}); code != 0 || !strings.Contains(out.String(), "contents "+id) {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), errs.String())
		}
	}
}
