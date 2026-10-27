package guard

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegratedGuardExecHelper(t *testing.T) {
	if os.Getenv("IKESU_TEST_INTEGRATED_EXEC") != "1" {
		return
	}
	env := EnvMap(os.Environ())
	i := Input{Command: env["IKESU_TEST_INTEGRATED_TARGET"], Cwd: env["IKESU_TEST_INTEGRATED_ROOT"], Env: env}
	if command := env["IKESU_GUARD_PARENT_COMMAND"]; command != "" {
		fmt.Printf("INHERITED_COMMAND=%s\nINHERITED_REASON=%s\n", command, requestReason(i))
	}
	if err := Execute(i); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestIntegratedGuardRequestInheritanceAndRecursionStop(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint("nested=", nested), func(t *testing.T) {
			root := t.TempDir()
			first := filepath.Join(root, "first")
			second := filepath.Join(root, "second")
			bin := filepath.Join(root, "guard", "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			helper := quote(os.Args[0]) + " -test.run=^TestIntegratedGuardExecHelper$"
			script := "#!/bin/sh\nprintf 'VISIT\\n'\nIKESU_TEST_VISITS=$(( ${IKESU_TEST_VISITS:-0} + 1 )); export IKESU_TEST_VISITS\nif [ \"$IKESU_TEST_VISITS\" -ge 3 ]; then exit 93; fi\n"
			if nested {
				script += "IKESU_TEST_INTEGRATED_TARGET=" + quote(second) + " " + helper + "\n"
			} else {
				script += "exec " + helper + "\n"
			}
			if err := os.WriteFile(first, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, []byte("#!/bin/sh\nprintf 'NESTED_OK\\nREASON=%s\\n' \"$IKESU_GUARD_PARENT_REASON\"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestIntegratedGuardExecHelper$")
			cmd.Env = append(childenv.Scrub(os.Environ()), "PATH=/usr/bin:/bin", "IKESU_GUARD_BIN="+bin, "IKESU_TEST_INTEGRATED_EXEC=1", "IKESU_TEST_INTEGRATED_ROOT="+root, "IKESU_TEST_INTEGRATED_TARGET="+first, "AI_GUARD_REQUEST_REASON=統合試験の申請文")
			out, err := cmd.CombinedOutput()
			text := string(out)
			if !strings.Contains(text, "INHERITED_COMMAND="+first) || !strings.Contains(text, "INHERITED_REASON=統合試験の申請文") {
				t.Fatal("request lost at real exec boundary", text, err)
			}
			if nested {
				if err != nil || !strings.Contains(text, "NESTED_OK\nREASON=統合試験の申請文") {
					t.Fatal(text, err)
				}
			} else if err == nil || strings.Count(text, "VISIT\n") != 1 || !strings.Contains(text, "身代わりスクリプトが自分自身を呼び直しました") {
				t.Fatal("direct reentry was not stopped after inheriting the request", text, err)
			}
		})
	}
}
