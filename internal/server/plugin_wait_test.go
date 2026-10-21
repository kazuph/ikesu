package server

import (
	"bytes"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/cli"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestAdditiveWaitCLIRealServer(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	apiOK(t, dir, "pane.report_agent", map[string]any{"pane_id": "p1", "source": "custom:wait-test", "agent": "qwen", "state": "blocked"}, nil)
	cases := []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"wait", "agent-status", "p1", "--status", "blocked", "--timeout", "0"}, 0, "p1 reached status blocked"},
		{[]string{"wait", "agent-status", "p1", "--status", "idle", "--timeout", "0"}, 1, "timeout waiting"},
		{[]string{"wait", "output", "p1", "--match", ".", "--regex", "--source", "visible", "--timeout", "1000"}, 0, "output_matched"},
		{[]string{"wait", "output", "p1", "--match", "NEVER_PRINTED_WAIT_PROOF", "--source", "recent", "--timeout", "0"}, 1, "timeout"},
		{[]string{"wait", "output", "p1", "--match", ".", "--regex", "--timeout", "1000"}, 0, "output_matched"},
		{[]string{"wait", "output", "p1", "--match", ".", "--regex", "--source", "visible", "--raw", "--timeout", "1000"}, 0, "output_matched"},
	}
	for _, tc := range cases {
		var out, err bytes.Buffer
		code := cli.Run(tc.args, cli.Options{Session: "test", Explicit: true, Stdout: &out, Stderr: &err})
		if code != tc.code || !bytes.Contains(append(out.Bytes(), err.Bytes()...), []byte(tc.contains)) {
			t.Fatalf("%v: code=%d out=%s err=%s", tc.args, code, &out, &err)
		}
		if tc.code == 0 && tc.args[1] == "output" {
			var response api.Response
			if e := json.Unmarshal(out.Bytes(), &response); e != nil || response.Err != nil {
				t.Fatalf("invalid response %s: %v", &out, e)
			}
		}
	}
}
