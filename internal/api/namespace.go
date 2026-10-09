package api

import (
	"path/filepath"
	"strings"
)

// Configured by main before any goroutines start.
var binaryNamespace = "gtx"

func ConfigureExecutable(path string) { binaryNamespace = ExecutableNamespace(path) }
func BinaryNamespace() string         { return binaryNamespace }

func ExecutableNamespace(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var out strings.Builder
	for _, c := range stem {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			out.WriteRune(c)
		}
	}
	if out.Len() == 0 {
		return "gtx"
	}
	return out.String()
}

func stateRoot(namespace string, getenv func(string) string, home string) string {
	if dir := getenv("GTX_STATE_DIR"); dir != "" {
		if namespace == "gtx" {
			return dir
		}
		return filepath.Join(dir, namespace)
	}
	return filepath.Join(home, ".local", "state", namespace)
}

func inheritedSocket(namespace, path string, explicit bool) bool {
	if explicit {
		return true
	}
	for _, component := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if component == namespace {
			return true
		}
		if strings.HasPrefix(component, "gtx-") {
			return false
		}
	}
	return namespace == "gtx"
}
