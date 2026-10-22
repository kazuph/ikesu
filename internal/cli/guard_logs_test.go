package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardLogStatusAndCompareCLI(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	for k, v := range map[string]string{"HOME": d, "IKESU_CONFIG": filepath.Join(d, "config.toml"), "IKESU_STATE_DIR": filepath.Join(d, "state"), "IKESU_GUARD_MARKER": "", "IKESU_SOCKET_PATH": filepath.Join(d, "absent.sock")} {
		teststate.Setenv(t, k, v)
	}
	path := filepath.Join(api.StateDir(), "guard", "decisions.jsonl")
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	var records strings.Builder
	for n := 0; n < 25; n++ {
		fmt.Fprintf(&records, "{\"ts_epoch\":1767225600,\"cmd_line\":\"echo %d\",\"verdict\":\"allow\",\"rule\":\"default\"}\n", n)
	}
	if e := os.WriteFile(path, []byte(records.String()), 0600); e != nil {
		t.Fatal(e)
	}
	run := func(a ...string) (int, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(append([]string{"guard"}, a...), Options{Stdout: &out, Stderr: &errs})
		if errs.Len() != 0 {
			t.Fatal(errs.String())
		}
		return code, out.String()
	}
	code, out := run("status")
	var status map[string]any
	if e := json.Unmarshal([]byte(out), &status); e != nil || code != 0 {
		t.Fatal(code, out, e)
	}
	if len(status) != 7 || status["log_records"] != float64(25) || status["ask_timeout_seconds"] != float64(300) || status["marker"] != nil || status["mode"] != "enforce" || status["log_path"] != path {
		t.Fatal(status)
	}
	code, out = run("log", "--json")
	if code != 0 || strings.Count(out, "\n") != 20 || strings.Contains(out, `"echo 4"`) || !strings.Contains(out, `"echo 5"`) {
		t.Fatal(code, out)
	}
	code, out = run("log", "--lines", "0")
	if code != 0 || out != "" {
		t.Fatal(code, out)
	}
	code, out = run("log", "--lines", "1")
	if code != 0 || !strings.Contains(out, "allow\tdefault\techo 24") {
		t.Fatal(code, out)
	}
	g, ai := filepath.Join(d, "g.jsonl"), filepath.Join(d, "ai.tsv")
	if e := os.WriteFile(g, []byte("{\"ts_epoch\":1767225600,\"cmd_line\":\"echo hi\",\"verdict\":\"allow\"}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(ai, []byte("2027-01-01T00:00:00Z\tALLOW\techo hi\n"), 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"compare", "--guard-log", g, "--ai-guard-log", ai}
	code, out = run(args...)
	if code != 0 || !strings.Contains(out, "AGREE") || strings.Contains(out, "OUT-OF-WINDOW") {
		t.Fatal(code, out)
	}
	code, out = run(append(args, "--window", "0")...)
	if code != 0 || !strings.Contains(out, "OUT-OF-WINDOW") {
		t.Fatal(code, out)
	}
	if e := os.WriteFile(ai, []byte("2027-01-01T00:00:00Z\tREJECT\techo hi\n"), 0600); e != nil {
		t.Fatal(e)
	}
	code, out = run(args...)
	if code != 1 || !strings.Contains(out, "DIVERGE") {
		t.Fatal(code, out)
	}
}
