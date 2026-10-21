package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestParityJobLogRealStreamsAndMetadata(t *testing.T) {
	teststate.Lock(t)
	dir, _ := startTestServer(t)
	dialClient(t, dir)
	var started api.RunStartedResult
	apiOK(t, dir, api.MethodRunStart, api.RunStartParams{CallerPane: firstPaneID(t, dir), Label: "log-streams",
		Argv:       []string{"/bin/sh", "-c", "printf 'PARITY_STDOUT\\n'; printf 'PARITY_STDERR\\n' >&2; exit 7"},
		Completion: "none"}, &started)
	store := openStore(t)
	deadline := time.Now().Add(10 * time.Second)
	for jobStatus(t, store, started.Job) != "exited" {
		if time.Now().After(deadline) {
			t.Fatal("real job did not exit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, err := store.CommandRow(started.Job)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(job.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "job_id: "+started.Job+"\nrunner_pid: ") {
		t.Fatal("metadata must precede real output", text)
	}
	for _, want := range []string{"command: '/bin/sh' '-c' ", "cwd: ", "started_unix_ms: ",
		"[stdout] PARITY_STDOUT\n", "[stderr] PARITY_STDERR\n", "finished_unix_ms: ", "exit_code: 7\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}
