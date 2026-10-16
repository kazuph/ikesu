package tui

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage7ClipboardSandboxFallback(t *testing.T) {
	teststate.Lock(t)
	if runtime.GOOS == "darwin" && os.Getenv("STAGE7_CLIPBOARD_SANDBOX") != "1" {
		cmd := exec.Command("/usr/bin/sandbox-exec", "-p", `(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))`, os.Args[0], "-test.run=^TestStage7ClipboardSandboxFallback$", "-test.v")
		cmd.Env = append(os.Environ(), "STAGE7_CLIPBOARD_SANDBOX=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated copy: %v %s", err, out)
		}
		return
	}
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		teststate.Setenv(t, name, "")
	}
	if runtime.GOOS == "darwin" {
		probe := exec.Command("/usr/bin/pbcopy")
		probe.Stdin = strings.NewReader("STAGE7_SANDBOX_PROBE")
		if probe.Run() == nil {
			t.Fatal("pasteboard sandbox did not deny pbcopy")
		}
	}
	var out bytes.Buffer
	a := &App{out: &out}
	if err := a.writeClipboard("alpha\nbeta"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\x1b]52;c;YWxwaGEKYmV0YQ==\a" {
		t.Fatalf("fallback=%q", out.String())
	}
	out.Reset()
	teststate.Setenv(t, "SSH_CONNECTION", "isolated-test")
	if err := a.writeClipboard("alpha\nbeta"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\x1b]52;c;YWxwaGEKYmV0YQ==\a" {
		t.Fatal("SSH did not use OSC 52")
	}
}
