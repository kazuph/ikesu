package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/filewatch"
	"github.com/kazuph/ikesu/internal/session"
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
	branch, _, _ := readGitBranch(cwd, nil)
	return branch
}

func readGitBranch(cwd string, watch *filewatch.Watcher) (string, bool, uint64) {
	paths := map[string]bool{}
	gitAncestors(cwd, paths)
	ready := watch != nil && watch.Update(gitPathList(paths))
	var revision uint64
	if watch != nil {
		revision = watch.Revision()
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		gitDir := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitDir); err == nil {
			if watch != nil {
				paths[gitDir] = true
				ready = watch.Update(gitPathList(paths)) && ready
			}
			if !info.IsDir() {
				b, err := os.ReadFile(gitDir)
				if err != nil {
					return "", ready, revision
				}
				path, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
				if !ok {
					return "", ready, revision
				}
				gitDir = path
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
			}
			if watch != nil {
				gitAncestors(gitDir, paths)
				for dir := filepath.Clean(gitDir); ; dir = filepath.Dir(dir) {
					if target, err := os.Readlink(dir); err == nil {
						if !filepath.IsAbs(target) {
							target = filepath.Join(filepath.Dir(dir), target)
						}
						gitAncestors(target, paths)
					}
					if resolved, err := filepath.EvalSymlinks(dir); err == nil {
						gitAncestors(resolved, paths)
					}
					if filepath.Dir(dir) == dir {
						break
					}
				}
				paths[filepath.Join(gitDir, "HEAD")] = true
				ready = watch.Update(gitPathList(paths)) && ready
			}
			b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return "", ready, revision
			}
			branch, _ := strings.CutPrefix(strings.TrimSpace(string(b)), "ref: refs/heads/")
			if strings.HasPrefix(string(b), "ref: refs/heads/") {
				return branch, ready, revision
			}
			return "", ready, revision
		}
		if filepath.Dir(dir) == dir {
			return "", ready, revision
		}
	}
}

type branchCacheEntry struct {
	watch    *filewatch.Watcher
	branch   string
	revision uint64
	ready    bool
}

func (a *App) cachedGitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	cwd = filepath.Clean(cwd)
	if a.branchCache == nil {
		a.branchCache = make(map[string]*branchCacheEntry)
	}
	entry := a.branchCache[cwd]
	if entry == nil {
		entry = &branchCacheEntry{watch: filewatch.New(nil)}
		a.branchCache[cwd] = entry
	}
	if entry.ready && entry.watch.Revision() == entry.revision {
		return entry.branch
	}
	entry.branch, entry.ready, entry.revision = readGitBranch(cwd, entry.watch)
	return entry.branch
}

func (a *App) pruneBranchCache(active map[string]bool) {
	for cwd, entry := range a.branchCache {
		if !active[cwd] {
			entry.watch.Close()
			delete(a.branchCache, cwd)
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
