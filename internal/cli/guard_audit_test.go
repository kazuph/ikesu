package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
	"github.com/kazuph/ikesu/internal/teststate"
)

// サーバーに繋がらなくても asks.jsonl だけで集計し、その旨を出す。--json と --html も出る。
func TestGuardAuditWithoutServer(t *testing.T) {
	teststate.Lock(t)
	d := t.TempDir()
	for k, v := range map[string]string{"HOME": d, "IKESU_CONFIG": filepath.Join(d, "config.toml"), "IKESU_STATE_DIR": filepath.Join(d, "state"), "IKESU_GUARD_MARKER": "", "IKESU_SOCKET_PATH": filepath.Join(d, "absent.sock")} {
		teststate.Setenv(t, k, v)
	}
	root := api.StateDir()
	now := time.Now()
	for _, outcome := range []string{guard.OutcomeApprove, guard.OutcomeApprove, guard.OutcomeReject} {
		if err := guard.AppendAsk(root, guard.AskRecord{TS: now.UTC().Format(time.RFC3339), TSEpoch: now.Unix() - 60, Rule: "http-write", Command: "curl -X POST https://e.com", Cwd: d, Outcome: outcome, WaitMS: 4000}); err != nil {
			t.Fatal(err)
		}
	}
	run := func(a ...string) (int, string, string) {
		t.Helper()
		var out, errs bytes.Buffer
		code := Run(append([]string{"guard", "audit"}, a...), Options{Stdout: &out, Stderr: &errs})
		return code, out.String(), errs.String()
	}
	code, out, _ := run("--since", "7d")
	if code != 0 || !strings.Contains(out, "全体: 3件") || !strings.Contains(out, "サーバーに繋がらない") || !strings.Contains(out, "http-write・curl・") || !strings.Contains(out, "妥当（却下あり）") {
		t.Fatal(code, out)
	}
	htmlPath := filepath.Join(d, "audit.html")
	code, out, errs := run("--json", "--html", htmlPath, "--top", "1", "--replay")
	var rep map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rep) != nil || rep["total"] != float64(3) || len(rep["top"].([]any)) != 1 || !strings.Contains(errs, htmlPath) {
		t.Fatal(code, out, errs)
	}
	if b, err := os.ReadFile(htmlPath); err != nil || !strings.Contains(string(b), "見逃し側の見本") {
		t.Fatalf("html: %v", err)
	}
	if code, _, errs := run("--since", "abc"); code == 0 || errs == "" {
		t.Fatal("bad --since accepted")
	}
	if code, out, _ := run("--since", "1h"); code != 0 || !strings.Contains(out, "全体: 3件") {
		t.Fatal(code, out)
	}
	if code, out, _ := run("--since", "30s"); code == 0 {
		t.Fatal("unit s accepted", out)
	}
}
