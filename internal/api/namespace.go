package api

import "path/filepath"

// Storage identity is independent of the executable's deployment filename.
func ConfigureExecutable(string)        {}
func BinaryNamespace() string           { return "ikesu" }
func ExecutableNamespace(string) string { return "ikesu" }

func stateRoot(_ string, getenv func(string) string, home string) string {
	if dir := getenv("IKESU_STATE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".local", "state", "ikesu")
}

func inheritedSocket(_ string, _ string, _ bool) bool { return true }
