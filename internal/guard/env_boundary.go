package guard

import (
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/childenv"
)

// envDropsGuard is reached when env itself is judged, as through its command
// shim. A child started with a cleared environment, a PATH without the shims
// or changed startup files runs without any guard, so it needs approval.
// Parsed command lines unwrap env first and keep their own checks.
func envDropsGuard(i Input, args []string) string {
	child := i
	child.Env = EnvMap(environment(i.Env))
	rest, err := envArguments(&child, args)
	if err != nil {
		return err.Error()
	}
	if len(rest) == 0 {
		return ""
	}
	const reason = "env で見張りの環境（PATH の身代わり・起動ファイル・目印）を外して起動するには承認が必要です"
	if child.ClaudePrefixChanged {
		return reason
	}
	original := environment(i.Env)
	kept := map[string]bool{}
	for _, dir := range filepath.SplitList(child.Env["PATH"]) {
		kept[filepath.Clean(dir)] = true
	}
	for _, dir := range filepath.SplitList(i.Env["PATH"]) {
		if childenv.IsGuardShimDirectory(dir, original) && !kept[filepath.Clean(dir)] {
			return reason
		}
	}
	for key, value := range i.Env {
		if (startupVariable(key) || strings.HasPrefix(key, "IKESU_GUARD_")) && child.Env[key] != value {
			return reason
		}
	}
	for key := range child.Env {
		if _, exists := i.Env[key]; !exists && (startupVariable(key) || strings.HasPrefix(key, "IKESU_GUARD_")) {
			return reason
		}
	}
	return ""
}
