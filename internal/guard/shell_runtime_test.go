package guard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The generated entry calls the real authorization implementation in a fresh
// process. No server is supplied: unsafe lines must fail closed on communication.
func TestShellRuntimeHelper(t *testing.T) {
	if os.Getenv("IKESU_SHELL_RUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args
	for n, arg := range args {
		if arg == "--via" && n+1 < len(args) && args[n+1] == "bash-debug" {
			i := WithShellVariables(Input{Cwd: os.Getenv("IKESU_RUNTIME_ROOT"), Env: EnvMap(os.Environ())}, os.Stdin)
			if !AuthorizeLine(Config{}, i.Cwd, i, args[len(args)-1], os.Stderr, nil) {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	// Whole-command checks use the same parser, without a variable snapshot.
	i := Input{Cwd: os.Getenv("IKESU_RUNTIME_ROOT"), Env: EnvMap(os.Environ())}
	if !AuthorizeLine(Config{}, i.Cwd, i, args[len(args)-1], os.Stderr, nil) {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestBashRuntimeSnapshotAndFailureStops(t *testing.T) {
	for name, shell := range map[string]string{"system": "/bin/bash", "homebrew": "/opt/homebrew/bin/bash"} {
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				if name == "homebrew" && os.IsNotExist(err) {
					t.Skip("標準の場所にHomebrew bashがないため、この版の実物テストだけ未実施")
				}
				t.Fatal("required bash missing", err)
			}
			version, err := exec.Command(shell, "--version").Output()
			if err != nil {
				t.Fatal(err)
			}
			t.Log(strings.Split(string(version), "\n")[0])
			for _, tc := range []struct {
				name, original, script string
				allow                  bool
			}{
				{"safe", "", `[[ $SHELLOPTS == *functrace* && ! ${_ikesu_guard_debug_busy+x} ]] || exit 72; OUT="$IKESU_RUNTIME_ROOT"; printf 'safe ran\n' > "$OUT/result"`, true},
				{"local", `f() { local OUT; OUT="$IKESU_RUNTIME_ROOT"; printf 'safe ran\n' > "$OUT/result"; }`, `OUT=/outside; f "$IKESU_RUNTIME_ROOT"`, true},
				{"unset", "", `OUT="$IKESU_RUNTIME_ROOT"; unset OUT; printf nope > "$OUT/result"`, false},
				{"spoof-mark", "", `_ikesu_guard_debug_busy=1; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"overwrite-function", "", `_ikesu_guard_debug() { :; }; T=/outside/victim; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-mark", "", `unset _ikesu_guard_debug_busy; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-function", "", `unset -f _ikesu_guard_debug; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"function", `f() { rm "$T"; };`, `T=/outside/victim; f; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"subshell", "", `T=/outside/victim; (rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"substitution", "", `T=/outside/victim; X=$(rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					entry := filepath.Join(root, "entry")
					adapter := "#!/bin/sh\nexec " + quote(os.Args[0]) + " -test.run=^TestShellRuntimeHelper$ -- \"$@\"\n"
					if err := os.WriteFile(entry, []byte(adapter), 0700); err != nil {
						t.Fatal(err)
					}
					orig := filepath.Join(root, "original")
					if err := os.WriteFile(orig, []byte("export ORIGINAL_LOADER_SEEN=1\n"+tc.original+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					a := NewAssets(root, "p_test", entry, Config{})
					env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p_test", "IKESU_SOCKET_PATH=" + root + "/no-server", "BASH_ENV=" + orig, "IKESU_RUNTIME_ROOT=" + root, "IKESU_SHELL_RUNTIME_HELPER=1"}, root)
					if err != nil {
						t.Fatal(err)
					}
					defer a.Close()
					if err := a.Sync("codex"); err != nil {
						t.Fatal(err)
					}
					script := filepath.Join(root, "script.sh")
					if err := os.WriteFile(script, []byte("[[ $ORIGINAL_LOADER_SEEN == 1 ]] || exit 71\n"+tc.script+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					// Apply the cap before loading BASH_ENV. The allowance matches
					// the approved 64-task cgroup probe and is local to this child.
					psArgs := []string{"-L", "-u", strconv.Itoa(os.Getuid()), "-o", "pid="}
					if runtime.GOOS == "darwin" {
						psArgs = []string{"-M", "-U", strconv.Itoa(os.Getuid()), "-o", "pid="}
					}
					tasks, err := exec.Command("/bin/ps", psArgs...).Output()
					if err != nil {
						t.Fatal(err)
					}
					limit := len(strings.Fields(string(tasks))) + 64
					bashEnv := EnvMap(env)["BASH_ENV"]
					clean := EnvMap(env)
					delete(clean, "BASH_ENV")
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, shell, "-c", `ulimit -u "$1" || exit; export BASH_ENV="$2"; exec "$3" "$4"`, "bounded-runtime", strconv.Itoa(limit), bashEnv, shell, script)
					cmd.Env = environment(clean)
					cmd.Dir = root
					cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
					cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
					cmd.WaitDelay = time.Second
					var out, stderr bytes.Buffer
					cmd.Stdout = &out
					cmd.Stderr = &stderr
					err = cmd.Run()
					if (err == nil) != tc.allow {
						t.Fatalf("allow=%v err=%v stderr=%s stdout=%s", tc.allow, err, stderr.String(), out.String())
					}
					if strings.Contains(stderr.String(), "bashdb") || strings.Contains(stderr.String(), "cannot start debugger") {
						t.Fatal(stderr.String())
					}
					if tc.allow {
						result, err := os.ReadFile(filepath.Join(root, "result"))
						if err != nil || string(result) != "safe ran\n" {
							t.Fatal(string(result), err, stderr.String())
						}
					} else if _, err := os.Stat(filepath.Join(root, "after")); !os.IsNotExist(err) {
						t.Fatal("post-failure line ran", err)
					}
					fmt.Println("runtime", shell, tc.name, "PASS")
				})
			}
		})
	}
}
