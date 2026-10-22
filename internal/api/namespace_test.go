package api

import "testing"

func TestExecutableIndependentStorage(t *testing.T) {
	for _, exe := range []string{"/tmp/ikesu", "/tmp/ikesu-xyz", "/tmp/foo"} {
		ConfigureExecutable(exe)
		if BinaryNamespace() != "ikesu" || ExecutableNamespace(exe) != "ikesu" {
			t.Fatal(exe)
		}
		if got := stateRoot(ExecutableNamespace(exe), func(string) string { return "" }, "/home"); got != "/home/.local/state/ikesu" {
			t.Fatal(got)
		}
	}
}
