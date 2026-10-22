package childenv

import (
	"os"
	"path/filepath"
	"strings"
)

// IsGuardShimDirectory recognizes generated PATH entries by state layout,
// including other pane/namespace bins after their marker variables were lost.
func IsGuardShimDirectory(path string, env []string) bool {
	for _, candidate := range []string{filepath.Clean(path), physicalPath(path)} {
		parts := strings.Split(filepath.ToSlash(candidate), "/")
		for i := 0; i+4 < len(parts); i++ {
			namespace := parts[i+2]
			known := namespace == "ikesu" || strings.HasPrefix(namespace, "ikesu-") || namespace == "herdr" || strings.HasPrefix(namespace, "herdr-")
			if parts[i] == ".local" && parts[i+1] == "state" && known && parts[i+3] == "guard" && parts[i+4] == "bin" {
				return true
			}
		}
	}
	values := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			values[k] = v
		}
	}
	for _, key := range []string{"IKESU_GUARD_BIN", "HERDR_GUARD_BIN", "HERDR_GUARD_SHIM_DIR"} {
		if value := values[key]; value != "" && physicalPath(path) == physicalPath(value) {
			return true
		}
	}
	home := values["HOME"]
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	roots := []string{values["IKESU_STATE_DIR"], filepath.Join(home, ".local", "state")}
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, pair := range [][2]string{{filepath.Clean(root), filepath.Clean(path)}, {physicalPath(root), physicalPath(path)}} {
			relative, err := filepath.Rel(pair[0], pair[1])
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				continue
			}
			parts := strings.Split(filepath.ToSlash(relative), "/")
			for i := 0; i+1 < len(parts); i++ {
				if parts[i] == "guard" && parts[i+1] == "bin" {
					return true
				}
			}
		}
	}
	return false
}
func physicalPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		return absolute
	}
	return filepath.Clean(path)
}
