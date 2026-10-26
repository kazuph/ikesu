package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRejectsRelocatedForwardingShims(t *testing.T) {
	for _, binary := range []string{"ikesu", "herdr"} {
		t.Run(binary, func(t *testing.T) {
			root := t.TempDir()
			shim := filepath.Join(root, "unknown-location")
			real := filepath.Join(root, "real")
			for _, dir := range []string{shim, real} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			forwarding := "#!/bin/sh\nIKESU_PANE_ID='p1' exec '/somewhere/" + binary + "' guard exec --via shim -- 'git' \"$@\"\n"
			entry := filepath.Join(shim, "git")
			if err := os.WriteFile(entry, []byte(forwarding), 0700); err != nil {
				t.Fatal(err)
			}
			realGit := filepath.Join(real, "git")
			if err := os.WriteFile(realGit, []byte("#!/bin/sh\nprintf 'real git\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			// Deliberately lose the original shim location, as after the incident.
			env := []string{"PATH=" + shim + ":" + real, "IKESU_GUARD_BIN=" + filepath.Join(root, "current-shim")}
			got, err := Resolve("git", env)
			if err != nil || got != realGit {
				t.Fatalf("resolved %q, %v; want real command %q", got, err, realGit)
			}
			for _, direct := range []string{entry, filepath.Join(root, "alias")} {
				if direct != entry {
					if err := os.Symlink(entry, direct); err != nil {
						t.Fatal(err)
					}
				}
				if got, err := Resolve(direct, env); err == nil || !strings.Contains(err.Error(), "guard shim") {
					t.Fatalf("accepted forwarding shim %q: %q, %v", direct, got, err)
				}
			}
		})
	}
}

// This helper runs only inside the bounded test subprocess. It simulates the
// lost locator in Execute's input, while the enclosing shell retains its guard bin.
func TestShimRecursionHelper(t *testing.T) {
	if os.Getenv("IKESU_TEST_EXEC_HELPER") != "1" {
		return
	}
	env := EnvMap(os.Environ())
	delete(env, "IKESU_GUARD_BIN")
	if err := Execute(Input{Command: env["IKESU_TEST_TARGET"], Args: []string{"--version"}, Cwd: env["IKESU_TEST_ROOT"], Env: env}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func TestExecuteStopsDirectShimReentryButAllowsNestedCommands(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint("nested=", nested), func(t *testing.T) {
			root := t.TempDir()
			first := filepath.Join(root, "first")
			second := filepath.Join(root, "second")
			helper := quote(os.Args[0]) + " -test.run=^TestShimRecursionHelper$"
			script := "#!/bin/sh\nIKESU_TEST_VISITS=$(( ${IKESU_TEST_VISITS:-0} + 1 )); export IKESU_TEST_VISITS\nprintf 'VISIT\\n'\nif [ \"$IKESU_TEST_VISITS\" -ge 3 ]; then exit 93; fi\nexec " + helper + "\n"
			if nested {
				script = "#!/bin/sh\nIKESU_TEST_TARGET=" + quote(second) + " " + helper + "\n"
			}
			if err := os.WriteFile(first, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, []byte("#!/bin/sh\nprintf 'NESTED_OK\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestShimRecursionHelper$")
			cmd.Env = append(os.Environ(), "IKESU_TEST_EXEC_HELPER=1", "IKESU_TEST_ROOT="+root, "IKESU_TEST_TARGET="+first)
			output, err := cmd.CombinedOutput()
			if nested {
				if err != nil || !strings.Contains(string(output), "NESTED_OK") {
					t.Fatalf("normal child rejected: %s %v", output, err)
				}
			} else {
				if err == nil || strings.Count(string(output), "VISIT") != 1 || !strings.Contains(string(output), "身代わりスクリプトが自分自身を呼び直しました") {
					t.Fatalf("direct recursion not stopped once: %s %v", output, err)
				}
			}
		})
	}
}
