package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/goterrace/internal/session"
)

// gitBranch reads Git's public HEAD metadata without launching a process
// during frame composition. Detached HEAD has no branch name.
func gitBranch(runtimeCwd, savedCwd string) string {
	cwd := runtimeCwd
	if cwd == "" {
		cwd = savedCwd
	}
	if cwd == "" {
		return ""
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if !info.IsDir() {
				b, err := os.ReadFile(gitDir)
				if err != nil {
					return ""
				}
				path, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
				if !ok {
					return ""
				}
				gitDir = path
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
			}
			b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return ""
			}
			branch, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
			if strings.HasPrefix(string(b), "ref: refs/heads/") {
				return branch
			}
			return ""
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

func (a *App) paneRestoreLabel(pid int, agent string) (string, string) {
	rec := a.sessions[pid]
	if agent == "" && rec != nil {
		agent = rec.Agent
	}
	if agent == "" {
		return "", ""
	}
	if rec != nil && rec.Agent == agent && session.SafeSessionValue(rec.Kind, rec.Value) {
		return agent, "saved session"
	}
	return agent, "no saved session"
}
