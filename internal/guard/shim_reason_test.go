package guard_test

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
	"github.com/kazuph/ikesu/internal/guard"
)

func TestShimRequestReasonWithRealServer(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "ikesu-guard-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "ikesu")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/ikesu")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("[experimental]\nallow_nested=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	socket := api.APISocketPath(filepath.Join(state, "default"))
	env := []string{"HOME=" + root, "PATH=/usr/bin:/bin", "SHELL=/bin/sh", "TMPDIR=" + root, "IKESU_CONFIG=" + config, "IKESU_STATE_DIR=" + state, "IKESU_SOCKET_PATH=" + socket, "IKESU_SOCKET_PATH_EXPLICIT=1"}
	command := func(env []string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env, cmd.Dir, cmd.WaitDelay = env, root, 10*time.Second
		return cmd
	}
	log, err := os.Create(filepath.Join(root, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	server := command(env, "server")
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		stop := exec.CommandContext(stopCtx, binary, "server", "stop")
		stop.Env = env
		if out, err := stop.CombinedOutput(); err != nil {
			t.Errorf("stop: %v: %s", err, out)
			cancel()
		}
		if err := server.Wait(); err != nil {
			b, _ := os.ReadFile(log.Name())
			t.Errorf("server: %v: %s", err, b)
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := command(env, "server", "ping").CombinedOutput(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server readiness deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
	listPending := func() []api.Decision {
		out, err := command(env, "decision", "list", "--status", "pending").CombinedOutput()
		if err != nil {
			t.Fatalf("list: %v: %s", err, out)
		}
		var reply struct{ Result api.DecisionListResult }
		if err := json.Unmarshal(out, &reply); err != nil {
			t.Fatal(err, string(out))
		}
		return reply.Result.Decisions
	}
	for _, key := range []string{"AI_GUARD_REQUEST_REASON", "PROJECT_APPROVAL_REASON"} {
		t.Run(key, func(t *testing.T) {
			a := guard.NewAssets(state, "p1", binary, guard.Config{})
			prepared, err := a.Prepare(append(append([]string{}, env...), "IKESU_PANE_ID=p1"), root)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Sync("codex"); err != nil {
				t.Fatal(err)
			}
			for _, reason := range []string{"", " \t\n\u3000", "Please approve this command", "日本語の申請文を試験する。実行せず承認待ちを取り消す。"} {
				t.Run(reason, func(t *testing.T) {
					childEnv := append(append([]string{}, prepared...), key+"="+reason)
					if key != "AI_GUARD_REQUEST_REASON" {
						childEnv = append(childEnv, "AI_GUARD_REQUEST_ENV_NAME="+key, "AI_GUARD_REQUEST_REASON=別の変数は読まない")
					}
					// Run the generated shell shim, which calls guard exec --via shim.
					cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "exec \"$@\"", "sh", filepath.Join(a.Bin, "dd"), "if=/dev/null", "of="+filepath.Join(root, "executed"))
					cmd.Env, cmd.Dir, cmd.WaitDelay = childEnv, root, 10*time.Second
					var out bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &out
					if !guard.ValidReason(reason) {
						if err := cmd.Run(); err == nil || !strings.Contains(out.String(), "例: "+key+"='") {
							t.Fatalf("expected hint and cancellation: %v: %s", err, &out)
						}
						if got := listPending(); len(got) != 0 {
							t.Fatalf("invalid reason created decisions: %+v", got)
						}
					} else {
						if err := cmd.Start(); err != nil {
							t.Fatal(err)
						}
						finished := make(chan error, 1)
						go func() { finished <- cmd.Wait() }()
						deadline := time.Now().Add(10 * time.Second)
						for {
							pending := listPending()
							if len(pending) == 1 {
								d := pending[0]
								if d.Kind != "command_guard" || d.Status != "pending" || d.Origin == nil || d.Origin.Reason != reason || len(d.Options) != 3 {
									t.Fatalf("approval contract changed: %+v", d)
								}
								t.Logf("pending: %+v", d)
								if b, err := command(env, "decision", "cancel", d.DecisionID).CombinedOutput(); err != nil {
									t.Fatalf("cancel: %v: %s", err, b)
								}
								break
							}
							if time.Now().After(deadline) {
								t.Fatal("pending decision deadline")
							}
							time.Sleep(20 * time.Millisecond)
						}
						select {
						case err := <-finished:
							if err == nil || !strings.Contains(out.String(), "判断待ちが承認されませんでした (cancelled)") {
								t.Fatalf("cancelled decision executed: %v: %s", err, &out)
							}
						case <-time.After(10 * time.Second):
							t.Fatal("shim exit deadline")
						}
					}
					t.Log(out.String())
					if _, err := os.Stat(filepath.Join(root, "executed")); !os.IsNotExist(err) {
						t.Fatalf("command ran: %v", err)
					}
				})
			}
		})
	}
}
