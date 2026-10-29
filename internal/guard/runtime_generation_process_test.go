package guard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func generationProcessInput(root string) Input {
	return Input{Env: map[string]string{"IKESU_GUARD_MARKER": filepath.Join(root, "guard", "agent-panes", "p_1.json"), "IKESU_PANE_ID": "p1"}}
}
func TestRuntimeGenerationProcessHelper(t *testing.T) {
	mode := os.Getenv("ENTRY_GENERATION_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("ENTRY_GENERATION_ROOT")
	i := generationProcessInput(root)
	switch mode {
	case "reader":
		lease, err := openRuntimeGeneration(i)
		if err != nil {
			t.Fatal(err)
		}
		old, err := os.ReadFile(filepath.Join(lease.path, "bash-env.current"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("VALIDATED " + lease.path)
		input := bufio.NewReader(os.Stdin)
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		lease.close()
		fmt.Println("RELEASED")
		if _, err := input.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		pinned, err := os.ReadFile(filepath.Join(lease.path, "bash-env.current"))
		if err != nil || !bytes.Equal(old, pinned) {
			t.Fatal("old reader changed generation", err)
		}
		if _, err := StartupGeneration(i); err == nil {
			t.Fatal("stale executable published after newer generation")
		}
		fmt.Println("STALE_REFUSED")
	case "publisher":
		m, _, err := ReadMarker(i)
		if err != nil {
			t.Fatal(err)
		}
		own, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		a := NewAssets(root, "p_1", own, *m.Policy)
		a.meta = m
		fmt.Println("PUBLISHING")
		if err := a.publishRuntime(); err != nil {
			t.Fatal(err)
		}
	case "refresh":
		path, err := StartupGeneration(i)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("GEN " + path)
	default:
		t.Fatal("unknown generation helper mode")
	}
}

func TestRuntimeGenerationCrossProcessPublication(t *testing.T) {
	i, a := scriptPane(t)
	root := a.Root
	old, err := StartupGeneration(i)
	if err != nil {
		t.Fatal(err)
	}
	native, err := os.ReadFile(a.Executable)
	if err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(t.TempDir(), "next-guard-test")
	if err := os.WriteFile(next, native, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	makeChild := func(binary, mode string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestRuntimeGenerationProcessHelper$")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root, "ENTRY_GENERATION_ROOT=" + root, "ENTRY_GENERATION_MODE=" + mode, "GO_TEST_RUN_OWNER=" + os.Getenv("GO_TEST_RUN_OWNER")}
		t.Cleanup(func() {
			if cmd.Process != nil && cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
		return cmd
	}
	reader := makeChild(a.Executable, "reader")
	input, err := reader.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := reader.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var readerError bytes.Buffer
	reader.Stderr = &readerError
	if err := reader.Start(); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(output)
	if !scan.Scan() || scan.Text() != "VALIDATED "+old {
		t.Fatalf("reader did not validate generation: %q %s", scan.Text(), readerError.String())
	}
	publisher := makeChild(next, "publisher")
	published, err := publisher.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	started := bufio.NewScanner(published)
	if !started.Scan() || started.Text() != "PUBLISHING" {
		t.Fatalf("publisher did not start: %q", started.Text())
	}
	// The actual filesystem lock is unavailable while the previous executable
	// owns its validated lease. No timer is used as the ordering proof.
	shell, err := a.privateShell(false)
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	fd, err := unix.Openat(shell.fd(), "generation.lock", unix.O_RDWR|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("validation did not hold publication lock: %v", err)
	}
	if _, err := input.Write([]byte("release\n")); err != nil {
		t.Fatal(err)
	}
	if !scan.Scan() || scan.Text() != "RELEASED" {
		t.Fatalf("reader did not release: %q", scan.Text())
	}
	for started.Scan() {
	}
	if err := publisher.Wait(); err != nil {
		t.Fatal("publisher failed", err)
	}
	now, err := os.Readlink(filepath.Join(filepath.Dir(a.RuntimePath()), "current"))
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(shell.path, now)
	if current == old {
		t.Fatal("new executable did not publish a new generation")
	}
	if _, err := input.Write([]byte("after-swap\n")); err != nil {
		t.Fatal(err)
	}
	if !scan.Scan() || scan.Text() != "STALE_REFUSED" {
		t.Fatalf("stale CLI was not refused: %q %s", scan.Text(), readerError.String())
	}
	for scan.Scan() {
	}
	if err := reader.Wait(); err != nil {
		t.Fatal(err, readerError.String())
	}
	// Concurrent same-version refreshes retain the exact generation and files.
	before, err := os.Stat(filepath.Join(current, "bash-env.current"))
	if err != nil {
		t.Fatal(err)
	}
	commands := []*exec.Cmd{makeChild(next, "refresh"), makeChild(next, "refresh"), makeChild(next, "refresh"), makeChild(next, "refresh")}
	outputs := make([]bytes.Buffer, len(commands))
	for n, cmd := range commands {
		cmd.Stdout = &outputs[n]
		cmd.Stderr = &outputs[n]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for n, cmd := range commands {
		if err := cmd.Wait(); err != nil || !strings.Contains(outputs[n].String(), "GEN "+current) {
			t.Fatalf("concurrent refresh: %v %s", err, outputs[n].String())
		}
	}
	after, err := os.Stat(filepath.Join(current, "bash-env.current"))
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("same generation rewritten", err)
	}
}
