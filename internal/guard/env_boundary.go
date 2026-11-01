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
	// Command lookup is ordered. The child PATH must begin with the original
	// entries up to the last shim directory, so nothing can resolve ahead of
	// a shim; entries after that prefix may change freely.
	original := environment(i.Env)
	before := filepath.SplitList(i.Env["PATH"])
	guarded := 0
	for n, dir := range before {
		if childenv.IsGuardShimDirectory(dir, original) {
			guarded = n + 1
		}
	}
	after := filepath.SplitList(child.Env["PATH"])
	if len(after) < guarded {
		return reason
	}
	for n := 0; n < guarded; n++ {
		if filepath.Clean(after[n]) != filepath.Clean(before[n]) {
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
