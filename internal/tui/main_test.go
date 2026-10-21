package tui

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

const testPasteboardSandbox = `(version 1)(allow default)(deny mach-lookup (global-name "com.apple.pasteboard.1"))`

func TestMain(m *testing.M) {
	if runtime.GOOS == "darwin" && os.Getenv("STAGE7_TUI_TEST_SANDBOX") != "1" {
		cmd := exec.Command("/usr/bin/sandbox-exec", append([]string{"-p", testPasteboardSandbox, os.Args[0]}, os.Args[1:]...)...)
		cmd.Env = append(os.Environ(), "STAGE7_TUI_TEST_SANDBOX=1")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(teststate.Run(m.Run))
}
