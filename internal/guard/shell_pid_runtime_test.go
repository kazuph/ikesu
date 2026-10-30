package guard

import (
	"context"
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

func TestBashRuntimePIDStatusline(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "ikesu")
	if out, err := exec.Command("go", "build", "-o", binary, "../../cmd/ikesu").CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	a := NewAssets(root, "p_1", binary, Config{})
	env, err := a.Prepare([]string{"HOME=" + root, "PATH=" + filepath.Dir(jq) + ":/usr/bin:/bin", "IKESU_PANE_ID=p1", "IKESU_SOCKET_PATH=" + root + "/no-server"}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Sync("codex"); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "statusline.sh")
	body := `RL_CACHE="$HOME/cache"; input='{"rate_limits":{"x":1}}'; echo "$input" | jq -c '{rate_limits}' > "${RL_CACHE}.tmp.$$" 2>/dev/null; printf '%s' "$$"`
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
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
	clean := EnvMap(env)
	bashEnv := clean["BASH_ENV"]
	delete(clean, "BASH_ENV")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", `ulimit -u "$1" || exit; export BASH_ENV="$2"; exec /bin/bash "$3"`, "bounded-runtime", strconv.Itoa(limit), bashEnv, script)
	cmd.Env = environment(clean)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime: %v: %s", err, out)
	}
	pid := string(out)
	if pid == "" || strings.Trim(pid, "0123456789") != "" {
		t.Fatalf("invalid pid output: %q", pid)
	}
	data, err := os.ReadFile(filepath.Join(root, "cache.tmp."+pid))
	if err != nil || string(data) != "{\"rate_limits\":{\"x\":1}}\n" {
		t.Fatalf("output %q: %v", data, err)
	}
}
