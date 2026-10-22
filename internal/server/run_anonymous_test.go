package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestAnonymousRunAdmissionCLIAndRPC(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	store, err := dispatch.Open(dispatch.DBPath("test"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, completion := range []string{"none", "summary", "full"} {
		for _, caller := range []string{"", "p999999"} {
			before, _ := store.MaxJobSeq()
			response := apiReq(t, dir, api.MethodRunStart, api.RunStartParams{Completion: completion, CallerPane: caller, Argv: []string{"/usr/bin/true"}})
			wantOK := completion == "none" && caller == ""
			if (response.Err == nil) != wantOK {
				t.Fatalf("RPC %q/%q: %+v", completion, caller, response)
			}
			after, _ := store.MaxJobSeq()
			if !wantOK && before != after {
				t.Fatal("rejected RPC created a job")
			}
		}
		for _, paneMode := range []bool{false, true} {
			for _, caller := range []string{"", "p999999"} {
				before, _ := store.MaxJobSeq()
				args := []string{"run", "--completion", completion}
				if paneMode {
					args = append(args, "--pane")
				}
				if caller != "" {
					args = append(args, "--caller", caller)
				}
				args = append(args, "--", "/usr/bin/true")
				var out, stderr bytes.Buffer
				code := cli.Run(args, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &stderr, Getenv: func(key string) string {
					if key == "IKESU_STATE_DIR" {
						return api.StateDir()
					}
					return ""
				}})
				wantOK := completion == "none" && !paneMode && caller == ""
				if (code == 0) != wantOK {
					t.Fatalf("CLI %v: %d %s", args, code, &stderr)
				}
				after, _ := store.MaxJobSeq()
				if !wantOK && before != after {
					t.Fatal("rejected CLI created a job")
				}
				if wantOK {
					var result struct {
						Job string `json:"job"`
					}
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					job, err := store.CommandRow(result.Job)
					if err != nil || job == nil || job.CallerPane != "" || job.CallerAgent != "" {
						t.Fatal(job, err)
					}
				}
			}
		}
	}
}
