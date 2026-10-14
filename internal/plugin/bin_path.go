package plugin

import (
	"os"
	"path/filepath"
	"strings"
)

// Homebrew's opt and prefix links survive removal of a versioned Cellar.
func stableBinPath(path string) string {
	prefix, rest, ok := strings.Cut(filepath.Clean(path), string(filepath.Separator)+"Cellar"+string(filepath.Separator))
	if !ok {
		return path
	}
	parts := strings.Split(rest, string(filepath.Separator))
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] != "bin" || parts[3] == "" {
		return path
	}
	for _, candidate := range []string{filepath.Join(prefix, "opt", parts[0], "bin", parts[3]), filepath.Join(prefix, "bin", parts[3])} {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
	}
	return path
}
