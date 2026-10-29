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

	"golang.org/x/sys/unix"
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
	binary := filepath.Join(t.TempDir(), "ikesu")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real guard CLI: %v: %s", err, output)
	}
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

				{"ifs-colon", "", `IFS=:; OUT="$IKESU_RUNTIME_ROOT"; printf 'safe ran\n' > "$OUT/result"`, true},
				{"ifs-empty", "", `IFS=; OUT="$IKESU_RUNTIME_ROOT"; printf 'safe ran\n' > "$OUT/result"`, true},
				{"ignore-term", "", `trap '' TERM; T="$IKESU_RUNTIME_ROOT/home/../victim"; (rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"handle-term", "", `trap ':' TERM; T="$IKESU_RUNTIME_ROOT/home/../victim"; X=$(rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset", "", `OUT="$IKESU_RUNTIME_ROOT"; unset OUT; [[ ! ${OUT+x} ]]; OUT="$IKESU_RUNTIME_ROOT"; printf 'safe ran\n' > "$OUT/result"`, true},
				{"spoof-mark", "", `_ikesu_guard_debug_busy=1; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"overwrite-function", "", `_ikesu_guard_debug() { :; }; T=/outside/victim; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-mark", "", `unset _ikesu_guard_debug_busy; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-function", "", `unset -f _ikesu_guard_debug; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"eval-inner", "", `T="$IKESU_RUNTIME_ROOT/home/../victim"; eval 'printf before > "$IKESU_RUNTIME_ROOT/before"; rm "$T"'; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"source-inner", "", `T="$IKESU_RUNTIME_ROOT/home/../victim"; source "$IKESU_RUNTIME_SOURCE"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"substitution-inner", "", `T="$IKESU_RUNTIME_ROOT/home/../victim"; X=$(printf before > "$IKESU_RUNTIME_ROOT/before"; rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},

				{"dynamic-command-substitution", "", `$(printf /bin/rm) "$IKESU_RUNTIME_ROOT/victim"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"array-command-rm", "", `C=(/bin/rm); "${C[0]}" "$IKESU_RUNTIME_ROOT/home/../victim"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"array-command-echo", "", `C=(/bin/echo); "${C[0]}" ok; printf 'safe ran\n' > "$IKESU_RUNTIME_ROOT/result"`, true},
				{"dynamic-write-marker", "", `: > "$(printf %s "$IKESU_GUARD_MARKER")"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-arithmetic-env", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="$((BASH_ENV=0))"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-declare-internal", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; declare X="${_ikesu_guard_debug_busy:=1}"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-parameter-internal", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="${_ikesu_guard_debug_busy=1}"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-arithmetic-path", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="$((PATH=0))"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-arithmetic-ai", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="$((AI_GUARD_DANGER_WORD_ACK=1))"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-arithmetic-ikesu", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="$((IKESU_GUARD_ENFORCING=0))"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"rhs-parameter-ai", "", `printf 'state BASH_ENV=%s internal=%s\n' "$BASH_ENV" "${_ikesu_guard_debug_busy-unset}"; X="${AI_GUARD_DANGER_WORD_ACK:=1}"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"assignment-substitution", "", `T="$IKESU_RUNTIME_ROOT/home/../victim"; X=$(rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"integer-indirection", "", `declare -i N; N='_ikesu_guard_debug_busy=1'; T="$IKESU_RUNTIME_ROOT/home/../victim"; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"arithmetic-indirection", "", `N='_ikesu_guard_debug_busy=1'; (( N )); T="$IKESU_RUNTIME_ROOT/home/../victim"; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"overwrite-filter", "", `eval '_ikesu_guard_needs_check() { return 1; }'; T="$IKESU_RUNTIME_ROOT/home/../victim"; /bin/rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-filter", "", `unset -f _ikesu_guard_needs_check; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"spoof-filter-local", "", `_ikesu_guard_word=printf; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"eval-forward", "", `eval '_ikesu_guard_forward() { :; }'; T="$IKESU_RUNTIME_ROOT/home/../victim"; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"eval-command", "", `eval '_ikesu_guard_command() { :; }'; T="$IKESU_RUNTIME_ROOT/home/../victim"; command rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"source-forward", "", `source "$IKESU_RUNTIME_SOURCE"; T="$IKESU_RUNTIME_ROOT/home/../victim"; rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"define-command", "", `_ikesu_guard_command() { :; }; T="$IKESU_RUNTIME_ROOT/home/../victim"; command rm "$T"; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-forward", "", `unset -f _ikesu_guard_forward; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"unset-command", "", `unset -f _ikesu_guard_command; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"function", `f() { rm "$T"; };`, `T=/outside/victim; f; printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"subshell", "", `T=/outside/victim; (rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
				{"substitution", "", `T=/outside/victim; X=$(rm "$T"); printf after > "$IKESU_RUNTIME_ROOT/after"`, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					root := t.TempDir()
					if err := os.Mkdir(filepath.Join(root, "home"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, "victim"), []byte("unchanged"), 0600); err != nil {
						t.Fatal(err)
					}
					sourceFile := filepath.Join(root, "source.sh")
					sourceBody := "printf before > \"$IKESU_RUNTIME_ROOT/before\"\nrm \"$T\"\n"
					if tc.name == "source-forward" {
						sourceBody = "_ikesu_guard_forward() { :; }\n_ikesu_guard_command() { :; }\n"
					}
					if err := os.WriteFile(sourceFile, []byte(sourceBody), 0600); err != nil {
						t.Fatal(err)
					}

					entry := binary
					orig := filepath.Join(root, "original")
					if err := os.WriteFile(orig, []byte("export ORIGINAL_LOADER_SEEN=1\n"+tc.original+"\n"), 0600); err != nil {
						t.Fatal(err)
					}
					a := NewAssets(root, "p_1", entry, Config{})
					env, err := a.Prepare([]string{"HOME=" + root, "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/no-server", "BASH_ENV=" + orig, "IKESU_RUNTIME_ROOT=" + root, "IKESU_SHELL_RUNTIME_HELPER=1", "IKESU_RUNTIME_SOURCE=" + sourceFile}, root)
					if err != nil {
						t.Fatal(err)
					}
					defer a.Close()
					if err := a.Sync("codex"); err != nil {
						t.Fatal(err)
					}
					markerBefore, err := os.ReadFile(a.MarkerPath)
					if err != nil {
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
						psArgs = []string{"-U", strconv.Itoa(os.Getuid()), "-o", "pid="}
					}
					tasks, err := exec.Command("/bin/ps", psArgs...).Output()
					if err != nil {
						t.Fatal(err)
					}
					limit := len(strings.Fields(string(tasks))) + 64
					var inherited unix.Rlimit
					if err := unix.Getrlimit(unix.RLIMIT_NPROC, &inherited); err != nil {
						t.Fatal(err)
					}
					if inherited.Cur < uint64(limit) {
						limit = int(inherited.Cur)
					}
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
					if strings.HasPrefix(tc.name, "rhs-") {
						expected := "state BASH_ENV=" + bashEnv + " internal=unset\n"
						if out.String() != expected {
							t.Fatalf("reserved state before rejected expansion: got %q want %q", out.String(), expected)
						}
						if err == nil || ctx.Err() != nil {
							t.Fatalf("expansion was not synchronously rejected: %v", err)
						}
					}
					if strings.HasSuffix(tc.name, "-inner") {
						if data, err := os.ReadFile(filepath.Join(root, "before")); err != nil || string(data) != "before" {
							t.Fatal("shell container stopped before inner commands", err)
						}
					}
					markerAfter, markerErr := os.ReadFile(a.MarkerPath)
					if markerErr != nil || !bytes.Equal(markerBefore, markerAfter) {
						t.Fatalf("guard marker changed: %v", markerErr)
					}
					victim, err := os.ReadFile(filepath.Join(root, "victim"))
					if err != nil || string(victim) != "unchanged" {
						t.Fatal("disposable victim changed", err)
					}
					fmt.Println("runtime", shell, tc.name, "PASS")
				})
			}
		})
	}
}
