package cli

import (
	"bytes"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/teststate"
	"path/filepath"
	"testing"
)

func TestJobNoticeQueuesExactCallerWithoutServer(t *testing.T) {
	teststate.Lock(t)
	dir := t.TempDir()
	teststate.Setenv(t, "GTX_STATE_DIR", dir)
	var errors bytes.Buffer
	r := runner{o: Options{Session: "isolated", Getenv: testEnv(dir), Stderr: &errors}}
	r.notifyPaneJob("p42", "exact completion")
	r.notifyPaneJob("same-name", "rejected completion")
	r.notifyPaneJob("", "unknown caller")
	store, err := dispatch.Open(filepath.Join(dir, "sessions", "isolated", "gtx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.PendingJobMessages()
	if err != nil || len(pending) != 1 || pending[0].ToAgent != "p42" || pending[0].Body != "exact completion" {
		t.Fatal(pending, err, errors.String())
	}
	history, _ := store.History(dispatch.JobsRoom, nil, 10)
	if len(history) != 3 {
		t.Fatal(history)
	}
}
