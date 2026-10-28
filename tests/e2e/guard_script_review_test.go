package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestScriptReviewRealPreflight(t *testing.T) {
	s := start(t)
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "script-review-test", Agent: "claude", State: "idle"}, nil)
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	cases := []string{"privileged-inline", "builtin-read", "command-unset", "cd-pwd", "fake-bash-link", "fake-bash-file"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			view := *s
			view.t = t
			root := filepath.Join(s.stateDir, kind)
			if e := os.Mkdir(root, 0700); e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(root, "victim")
			if e := os.WriteFile(target, []byte("untouched"), 0600); e != nil {
				t.Fatal(e)
			}
			before := filepath.Join(root, "before")
			script := filepath.Join(root, "program.sh")
			command := "sh"
			path := "/usr/bin:/bin"
			body := ""
			switch kind {
			case "privileged-inline":
				body = "bash -p -c 'read TARGET; /bin/rm \"$TARGET\"'\n"
			case "builtin-read":
				body = "builtin read TARGET; /bin/rm \"$TARGET\"\n"
			case "command-unset":
				body = "command builtin unset TARGET; /bin/rm \"${TARGET:-" + target + "}\"\n"
			case "cd-pwd":
				body = "cd " + quote(root) + "; /bin/rm \"$PWD/victim\"\n"
			case "fake-bash-link", "fake-bash-file":
				command = "bash"
				path = root + ":" + path
				fake := filepath.Join(root, "bash")
				if kind == "fake-bash-link" {
					if e := os.Symlink("/bin/dash", fake); e != nil {
						t.Fatal(e)
					}
				} else {
					// A real executable with the name bash, which does not read BASH_ENV.
					if e := os.WriteFile(fake, []byte("#!/bin/sh\nexec /bin/dash \"$@\"\n"), 0700); e != nil {
						t.Fatal(e)
					}
				}
				body = "TARGET=" + quote(target) + "\n/bin/rm \"$TARGET\"\n"
			}
			body = "printf started > " + quote(before) + "\n" + body
			if e := os.WriteFile(script, []byte(body), 0600); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binPath, "--session", s.name, "guard", "claude-prefix", "--", command+" "+quote(script))
			cmd.Dir = root
			cmd.Env = []string{"HOME=" + root, "PATH=" + path, "SHELL=/bin/sh", "TARGET=/tmp/safe", "PWD=/tmp/safe", "OLDPWD=/tmp/safe", "IKESU_STATE_DIR=" + s.stateDir, "IKESU_PANE_ID=p1", "IKESU_CONFIG=" + filepath.Join(s.stateDir, "missing.toml"), "IKESU_GUARD_MARKER=" + filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json"), "IKESU_GUARD_EXECUTABLE=" + binPath, "BASH_ENV=" + filepath.Join(s.stateDir, "guard", "shell", "p_1", "bash-env"), "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesuレビュー修正。必要な理由: 抜け道が実行前に承認になることを検証。実施内容: 隔離した使い捨てファイルを対象にし却下する。"}
			cmd.Stdin = strings.NewReader(target + "\n")
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var pending api.DecisionListResult
			view.waitFor("review preflight approval", 5*time.Second, func(string) bool {
				view.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
				return len(pending.Decisions) == 1
			})
			d := pending.Decisions[0]
			if _, e := os.Stat(before); !os.IsNotExist(e) {
				t.Fatal("script started before approval")
			}
			if content, e := os.ReadFile(target); e != nil || string(content) != "untouched" {
				t.Fatal("target changed before approval", e)
			}
			if raw, e := view.cli("decision", "answer", d.DecisionID, "reject"); e != nil {
				t.Fatal(raw, e)
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("rejected command succeeded")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("rejection did not stop command")
			}
			if _, e := os.Stat(before); !os.IsNotExist(e) {
				t.Fatal("rejected script started")
			}
			if content, e := os.ReadFile(target); e != nil || string(content) != "untouched" {
				t.Fatal("target changed after rejection", e)
			}
			if dir := os.Getenv("SCRIPT_FILE_EVIDENCE_DIR"); dir != "" {
				b, e := json.MarshalIndent(d, "", "  ")
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(dir, "review-"+kind+".json"), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			t.Logf("%s: pending=%s, before absent, victim unchanged before/after reject; output=%s", kind, d.DecisionID, output.String())
		})
	}
}
